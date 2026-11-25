package server

import (
	"bytes"
	"os"
	"path/filepath"
)

//go:embed container-upgrade.sh
var containerUpgradeScript []byte

// containerUpgradeMarker marks the helper this panel may refresh. An official
// install.sh copy does not contain it and is left untouched.
const containerUpgradeMarker = "# kids-container-upgrade"

// EnsureContainerUpgradeHelper installs the container upgrader when it is
// missing. A later start refreshes only a helper this panel wrote before.
func EnsureContainerUpgradeHelper() error {
	return installContainerUpgradeHelper(nftUpgradePath)
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
