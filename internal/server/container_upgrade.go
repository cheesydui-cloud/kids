package server

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed container-upgrade.sh
var containerUpgradeScript []byte

//go:embed panel-start.sh
var panelStartScript []byte

// containerUpgradeMarker marks the helper this panel may refresh. An official
// install.sh copy does not contain it and is left untouched.
const containerUpgradeMarker = "# kids-container-upgrade"

// panelStartMarker marks the container entrypoint this panel may refresh.
// A real nft-server binary does not contain it and is left untouched.
const panelStartMarker = "# kids-panel-start"

// panelWrapperPath is the path Docker already executes. On overlayfs that file
// cannot be unlinked while it is the process image, so the host installer
// replaces it with panel-start.sh once, while the container is stopped.
var panelWrapperPath = "/usr/local/sbin/nft-server"

// panelStarterExtraPaths are starter copies the official image keeps beside
// the running program. Tests point these at missing files.
var panelStarterExtraPaths = []string{
	"/usr/local/lib/kids/panel-start.sh",
	"/usr/local/sbin/nft-server",
}

// panelReexecTarget is set when the new ELF was staged beside the running
// binary. The next exec must be the starter, not the busy inode.
var panelReexecTarget string

// EnsureContainerUpgradeHelper installs the container upgrader when it is
// missing. A later start refreshes only a helper this panel wrote before.
func EnsureContainerUpgradeHelper() error {
	if err := installContainerUpgradeHelper(nftUpgradePath); err != nil {
		return err
	}
	return refreshPanelStarter()
}

func installContainerUpgradeHelper(path string) error {
	existing, err := os.ReadFile(path)
	if err == nil && !bytes.Contains(existing, []byte(containerUpgradeMarker)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, containerUpgradeScriptBytes(), 0o755)
}

func containerUpgradeScriptBytes() []byte {
	return bytes.ReplaceAll(containerUpgradeScript, []byte("\r\n"), []byte("\n"))
}

func panelStartScriptBytes() []byte {
	return bytes.ReplaceAll(panelStartScript, []byte("\r\n"), []byte("\n"))
}

func refreshPanelStarter() error {
	p := panelStarterPath()
	if p == "" {
		return nil
	}
	return os.WriteFile(p, panelStartScriptBytes(), 0o755)
}

func looksLikeStarterFile(path string) bool {
	if path == "" {
		return false
	}
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(b, []byte(panelStartMarker))
}

// panelStarterPath finds the entrypoint script. Docker execs that script, then
// the script execs the ELF, so the script itself is not the busy inode.
func panelStarterPath() string {
	seen := map[string]struct{}{}
	candidates := []string{
		strings.TrimSpace(os.Getenv("KIDS_PANEL_STARTER")),
		panelWrapperPath,
	}
	candidates = append(candidates, panelStarterExtraPaths...)
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if looksLikeStarterFile(p) {
			return p
		}
	}
	return ""
}

func panelNextPath() string {
	return filepath.Join(panelUpdateDir, "nft-server.next")
}

func panelStarterReady() bool {
	return panelStarterPath() != ""
}

// stagePanelNext writes the downloaded ELF to a path that is not executed.
// The starter copies it onto a free slot before exec.
func stagePanelNext(data []byte) error {
	if err := os.MkdirAll(panelUpdateDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(panelUpdateDir, ".nft-server-next-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	dest := panelNextPath()
	if err := os.Rename(tmpName, dest); err != nil {
		if werr := os.WriteFile(dest, data, 0o755); werr != nil {
			_ = os.Remove(tmpName)
			return werr
		}
		_ = os.Remove(tmpName)
	}
	return os.Chmod(dest, 0o755)
}

// adoptStagedPanel keeps the running inode and asks the starter to switch.
func adoptStagedPanel(data []byte) error {
	if err := stagePanelNext(data); err != nil {
		return fmt.Errorf("无法替换正在运行的程序: %w", err)
	}
	p := panelStarterPath()
	if p == "" {
		return fmt.Errorf("正在运行的程序删不掉，新版本已写到 %s。入口还不是启动器，请在宿主机执行 install-container-panel.sh", panelNextPath())
	}
	panelReexecTarget = p
	return nil
}

func panelReexecPath() (string, error) {
	if p := panelStarterPath(); p != "" {
		return p, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("无法定位面板程序: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
		exe = resolved
	}
	return exe, nil
}

// preferInstallerUpgrade reports whether the detached upgrade should run the
// local nft-upgrade script. Containers, including ones that only have the
// helper script, replace the binary themselves. A systemd host keeps the
// official installer so the unit and the local agent move together.
func preferInstallerUpgrade(installerOK, systemd bool, script []byte) bool {
	if !installerOK || !systemd {
		return false
	}
	if bytes.Contains(script, []byte(containerUpgradeMarker)) {
		return false
	}
	return true
}

func shouldUseInstallerUpgrade() bool {
	script, _ := os.ReadFile(nftUpgradePath)
	return preferInstallerUpgrade(looksLikeInstallerFn(nftUpgradePath), panelRestartMode() == "systemd", script)
}
