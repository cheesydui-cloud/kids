package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const maxPanelBinaryBytes = 96 << 20

var (
	panelUpgradeGOOS   = runtime.GOOS
	panelUpgradeGOARCH = runtime.GOARCH
	panelUpgradeDelay  = 2 * time.Second

	downloadPanelBinaryFn = downloadPanelReleaseBinary
	replacePanelBinaryFn  = replaceExecutable
	reexecPanelFn         = schedulePanelReexec
	panelReleaseBaseFn    = defaultPanelReleaseBase

	releaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
)

func defaultPanelReleaseBase(tag string) string {
	return githubURL(fmt.Sprintf("https://github.com/%s/releases/download/%s", agentRepo, tag))
}

func panelAssetCandidates() []string {
	switch panelUpgradeGOARCH {
	case "amd64":
		return []string{"nft-server-linux-amd64", "nft-server"}
	case "arm64":
		return []string{"nft-server-linux-arm64"}
	default:
		return nil
	}
}

// startBuiltinPanelUpgrade downloads the published panel binary, replaces the
// running executable, and re-execs. Containers use this path. A systemd host
// with the official nft-upgrade script still uses that script.
func startBuiltinPanelUpgrade(target, current string) error {
	if err := os.MkdirAll(panelUpdateDir, 0o755); err != nil {
		return err
	}
	appendUpdateLog("未找到可用的 nft-upgrade，改由面板自己下载并替换\n")
	go func() {
		time.Sleep(panelUpgradeDelay)
		if err := runBuiltinPanelUpgrade(target); err != nil {
			msg := err.Error()
			appendUpdateLog("升级失败: " + msg + "\n")
			log.Printf("panel update: %s", msg)
			st := readPanelUpdateStatus()
			if st.Current == "" {
				st.Current = current
			}
			st.State = "error"
			st.Target = target
			st.Error = msg
			st.FinishedAt = time.Now().Unix()
			_ = writePanelUpdateStatus(st)
			return
		}
		if panelReexecTarget != "" {
			appendUpdateLog("新程序已放到旁边，正在通过启动器切换\n")
		} else {
			appendUpdateLog("二进制已替换，正在重新执行面板\n")
		}
		if err := reexecPanelFn(); err != nil {
			msg := "二进制已替换，但重新执行失败。请重启面板进程: " + err.Error()
			appendUpdateLog(msg + "\n")
			st := readPanelUpdateStatus()
			st.State = "error"
			st.Target = target
			st.Error = msg
			st.FinishedAt = time.Now().Unix()
			_ = writePanelUpdateStatus(st)
		}
	}()
	return nil
}

func runBuiltinPanelUpgrade(target string) error {
	appendUpdateLog("开始下载 " + target + "\n")
	data, asset, err := downloadPanelBinaryFn(target)
	if err != nil {
		return err
	}
	appendUpdateLog("已校验 " + asset + "，正在替换本机程序\n")
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位面板程序: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
		exe = resolved
	}
	if err := replacePanelBinaryFn(exe, data); err != nil {
		return err
	}
	return nil
}

func downloadPanelReleaseBinary(tag string) ([]byte, string, error) {
	if panelUpgradeGOOS != "linux" {
		return nil, "", fmt.Errorf("内置升级只支持 Linux 面板，当前是 %s/%s", panelUpgradeGOOS, panelUpgradeGOARCH)
	}
	if !releaseTagPattern.MatchString(tag) {
		return nil, "", fmt.Errorf("版本号无效: %s", tag)
	}
	names := panelAssetCandidates()
	if len(names) == 0 {
		return nil, "", fmt.Errorf("没有 linux/%s 的面板安装包", panelUpgradeGOARCH)
	}
	base := strings.TrimRight(panelReleaseBaseFn(tag), "/")
	var last error
	for _, name := range names {
		want, err := fetchSumFor(base+"/SHA256SUMS", name)
		if err != nil {
			last = err
			continue
		}
		data, err := httpGetLimited(base+"/"+name, maxPanelBinaryBytes)
		if err != nil {
			last = err
			continue
		}
		if len(data) < 1<<20 || len(data) < 4 || string(data[:4]) != "\x7fELF" {
			return nil, "", fmt.Errorf("下载 %s 失败: %s 不是面板程序", tag, name)
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, want) {
			return nil, "", fmt.Errorf("下载 %s 失败: %s sha256 校验失败", tag, name)
		}
		return data, name, nil
	}
	if last == nil {
		last = fmt.Errorf("未找到面板安装包")
	}
	return nil, "", fmt.Errorf("下载 %s 失败: %w", tag, last)
}

func httpGetLimited(url string, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: 180 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kids-panel")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("GET %s: body exceeds %d bytes", url, limit)
	}
	return data, nil
}

// replaceExecutable writes data over dest. A running binary on overlayfs often
// cannot be unlinked ("device or resource busy" / "text file busy"). The
// running process keeps the old inode either way. When unlink fails, the
// parent directory is swapped so the original path points at the new file.
func replaceExecutable(dest string, data []byte) error {
	panelReexecTarget = ""
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("无法写入新程序: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".nft-server-new-*")
	if err != nil {
		return fmt.Errorf("无法写入新程序: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("无法写入新程序: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("无法写入新程序: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("无法写入新程序: %w", err)
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		if !replaceErrBusy(err) {
			return fmt.Errorf("无法替换正在运行的程序: %w", err)
		}
		return replaceAfterBusy(dest, tmpName, data)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		if replaceErrBusy(err) || replaceErrCrossDevice(err) {
			return replaceAfterBusy(dest, tmpName, data)
		}
		if werr := os.WriteFile(dest, data, 0o755); werr != nil {
			return fmt.Errorf("替换失败且无法写回程序: %v / %w", err, werr)
		}
		return nil
	}
	cleanup = false
	return nil
}

func replaceAfterBusy(dest, tmpName string, data []byte) error {
	if serr := replaceByDirectorySwap(dest, tmpName); serr == nil {
		return nil
	} else {
		appendUpdateLog("目录替换失败: " + serr.Error() + "\n")
	}
	if xerr := replaceByExchange(dest, tmpName); xerr == nil {
		appendUpdateLog("已交换程序文件名\n")
		return nil
	}
	appendUpdateLog("正在运行的程序无法删除，改为写到旁边\n")
	return adoptStagedPanel(data)
}

func replaceErrBusy(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "device or resource busy") || strings.Contains(s, "text file busy")
}

func replaceErrCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "cross-device") || strings.Contains(s, "cross device")
}

// replaceByDirectorySwap puts src at dest's path without unlinking dest.
// Overlayfs returns EBUSY when the running binary is removed, but renaming
// the parent directory is allowed. The running process keeps the old inode.
func replaceByDirectorySwap(dest, src string) error {
	parent := filepath.Dir(dest)
	base := filepath.Base(dest)
	grand := filepath.Dir(parent)
	if parent == "" || parent == "." || parent == string(filepath.Separator) || grand == parent {
		return fmt.Errorf("无法移动目录 %s", parent)
	}
	newDir, err := os.MkdirTemp(grand, ".nft-sbin-new-")
	if err != nil {
		return err
	}
	moved := false
	defer func() {
		if !moved {
			_ = os.RemoveAll(newDir)
		}
	}()
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == base || strings.HasPrefix(e.Name(), ".nft-server-new-") {
			continue
		}
		if err := copyDirEntry(filepath.Join(parent, e.Name()), filepath.Join(newDir, e.Name())); err != nil {
			return err
		}
	}
	if err := copyFileMode(src, filepath.Join(newDir, base), 0o755); err != nil {
		return err
	}
	oldHold, err := os.MkdirTemp(grand, ".nft-sbin-old-")
	if err != nil {
		return err
	}
	if err := os.Remove(oldHold); err != nil {
		return err
	}
	if err := os.Rename(parent, oldHold); err != nil {
		return err
	}
	if err := os.Rename(newDir, parent); err != nil {
		_ = os.Rename(oldHold, parent)
		return err
	}
	moved = true
	_ = os.RemoveAll(oldHold)
	return nil
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func copyDirEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		return copyDirRecursive(src, dst, info.Mode().Perm())
	}
	return copyFileMode(src, dst, info.Mode().Perm())
}

func copyDirRecursive(src, dst string, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o755
	}
	if err := os.MkdirAll(dst, mode); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyDirEntry(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func appendUpdateLog(s string) {
	if err := os.MkdirAll(panelUpdateDir, 0o755); err != nil {
		log.Print(strings.TrimSpace(s))
		return
	}
	f, err := os.OpenFile(panelUpdateLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Print(strings.TrimSpace(s))
		return
	}
	_, _ = f.WriteString(s)
	_ = f.Close()
}
