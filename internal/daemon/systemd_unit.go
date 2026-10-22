package daemon

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// restartUnlimitedDropIn disables systemd's start-rate limit. The default
// cap (and the unit files shipped before this) stops Restart=always after
// a few fast failures. The node then stays offline until an operator runs
// systemctl reset-failed.
const restartUnlimitedDropIn = `# nft-agent: keep restarting after a burst of failures.
# A start-rate limit leaves the node offline until "systemctl reset-failed".
[Unit]
StartLimitIntervalSec=0
`

const (
	systemdUnitPath = "/etc/systemd/system/nft-daemon.service"
	systemdDropPath = "/etc/systemd/system/nft-daemon.service.d/restart-unlimited.conf"
)

// ensureUnlimitedSystemdRestarts installs the drop-in on machines that
// already have nft-daemon.service, then reloads systemd once. Missing
// unit (tests, tui-less dev hosts) is a no-op.
func ensureUnlimitedSystemdRestarts() {
	changed, err := writeRestartUnlimitedDropIn(systemdUnitPath, systemdDropPath)
	if err != nil {
		log.Printf("systemd: restart limit: %v", err)
		return
	}
	if !changed {
		return
	}
	out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput()
	if err != nil {
		log.Printf("systemd: daemon-reload: %v %s", err, strings.TrimSpace(string(out)))
	}
}

// writeRestartUnlimitedDropIn writes the drop-in when unitPath exists and
// the drop-in is missing or different. changed is false when nothing was
// written, including when the unit is not installed.
func writeRestartUnlimitedDropIn(unitPath, dropPath string) (changed bool, err error) {
	if _, err := os.Stat(unitPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if cur, err := os.ReadFile(dropPath); err == nil && string(cur) == restartUnlimitedDropIn {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dropPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(dropPath, []byte(restartUnlimitedDropIn), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
