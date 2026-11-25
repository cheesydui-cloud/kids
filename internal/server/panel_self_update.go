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
// running executable, and re-execs. It is the upgrade path for containers that
// were not installed with install.sh and therefore have no nft-upgrade script.
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
		appendUpdateLog("二进制已替换，正在重新执行面板\n")
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
			last = fmt.Errorf("%s 不是面板程序", name)
			continue
		}
		sum := sha256.Sum256(data)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, want) {
			last = fmt.Errorf("%s sha256 校验失败", name)
			continue
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

// replaceExecutable writes data over dest. A running binary cannot be renamed
// over on overlayfs (ETXTBSY, "device or resource busy"), so the path is
// unlinked first. The running process keeps the old inode until it re-execs.
func replaceExecutable(dest string, data []byte) error {
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
		return fmt.Errorf("无法替换正在运行的程序: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		if werr := os.WriteFile(dest, data, 0o755); werr != nil {
			return fmt.Errorf("替换失败且无法写回程序: %v / %w", err, werr)
		}
		return nil
	}
	cleanup = false
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
