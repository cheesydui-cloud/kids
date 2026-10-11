package daemon

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Upgrade rollback lives next to state. attempts counts process starts since
// the binary was replaced. A hello that stays up clears the file. The third
// start restores the previous executable onto disk but keeps running the new
// binary long enough to hello with upgrade_rolled_back, then restarts into
// the restored file. A panel that is merely down does not increment attempts.
const (
	upgradePendingName = "upgrade-pending.json"
	upgradeMarkerName  = "upgrade-rolled-back"
	upgradeAttemptMax  = 3
)

var (
	upgradeDir          = "/var/lib/nft"
	rollbackHelloWait   = 45 * time.Second
	restartFn           = restartSelf
	rollbackReport      atomic.Bool
	upgradeStaged       atomic.Bool
	rollbackBak         string
	rollbackGen         atomic.Uint64
	rollbackTimer       *time.Timer
	rollbackRestartOnce sync.Once
	upgradeStateMu      sync.Mutex
)

type upgradePending struct {
	Exe      string `json:"exe"`
	Bak      string `json:"bak"`
	Attempts int    `json:"attempts"`
	Version  string `json:"version"`
}

// SetUpgradeDir points rollback files at dir. Tests use a temp directory.
func SetUpgradeDir(dir string) {
	upgradeStateMu.Lock()
	upgradeDir = dir
	upgradeStateMu.Unlock()
}

func upgradeDirPath() string {
	upgradeStateMu.Lock()
	defer upgradeStateMu.Unlock()
	if upgradeDir == "" {
		return "/var/lib/nft"
	}
	return upgradeDir
}

func pendingPath() string { return filepath.Join(upgradeDirPath(), upgradePendingName) }
func markerPath() string  { return filepath.Join(upgradeDirPath(), upgradeMarkerName) }

// ResetUpgradeStateForTest drops in-memory rollback watches so a later test
// does not inherit a timer that would call restartFn.
func ResetUpgradeStateForTest() {
	rollbackGen.Add(1)
	if rollbackTimer != nil {
		rollbackTimer.Stop()
		rollbackTimer = nil
	}
	rollbackReport.Store(false)
	upgradeStaged.Store(false)
	rollbackBak = ""
	rollbackRestartOnce = sync.Once{}
}

func readPending() (*upgradePending, error) {
	b, err := os.ReadFile(pendingPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p upgradePending
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Exe == "" || p.Bak == "" {
		return nil, nil
	}
	return &p, nil
}

func writePending(p upgradePending) error {
	if err := os.MkdirAll(upgradeDirPath(), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(pendingPath(), b, 0o644)
}

func clearUpgradeArtifacts() {
	if p, err := readPending(); err == nil && p != nil && p.Bak != "" {
		_ = os.Remove(p.Bak)
	}
	if rollbackBak != "" {
		_ = os.Remove(rollbackBak)
		rollbackBak = ""
	}
	_ = os.Remove(pendingPath())
	_ = os.Remove(markerPath())
}

// stageUpgradeBackup copies the current executable aside and records a pending
// upgrade before the new bytes replace it. A failed replace must call
// clearUpgradePending so the next start does not treat the old binary as bad.
func stageUpgradeBackup(exePath, version string) error {
	bak := exePath + ".prev"
	if err := copyFile(exePath, bak); err != nil {
		return err
	}
	if err := writePending(upgradePending{Exe: exePath, Bak: bak, Attempts: 0, Version: version}); err != nil {
		_ = os.Remove(bak)
		return err
	}
	// This process already hello'd. A reconnect before it exits must not
	// clear the pending file the next process uses to decide on rollback.
	upgradeStaged.Store(true)
	return nil
}

func clearUpgradePending() {
	upgradeStaged.Store(false)
	if p, err := readPending(); err == nil && p != nil && p.Bak != "" {
		_ = os.Remove(p.Bak)
	}
	_ = os.Remove(pendingPath())
}

// RollbackIfUpgradeUnhealthy increments the pending start count. Below the
// third start it returns false and the process continues. On the third start
// it restores the previous binary onto disk, keeps this process running, and
// arms a hello deadline.
func RollbackIfUpgradeUnhealthy() bool {
	p, err := readPending()
	if err != nil || p == nil {
		return false
	}
	p.Attempts++
	if p.Attempts < upgradeAttemptMax {
		if err := writePending(*p); err != nil {
			log.Printf("upgrade: write pending attempts: %v", err)
		}
		log.Printf("upgrade: pending start %d/%d version=%s", p.Attempts, upgradeAttemptMax, p.Version)
		return false
	}
	if err := copyFile(p.Bak, p.Exe); err != nil {
		log.Printf("upgrade: rollback restore failed: %v", err)
		_ = writePending(*p)
		return false
	}
	rollbackBak = p.Bak
	_ = os.MkdirAll(upgradeDirPath(), 0o755)
	_ = os.WriteFile(markerPath(), []byte("1\n"), 0o644)
	// Drop pending so a crash before hello starts the restored binary instead
	// of rolling back again. The in-memory flag still reports this process.
	_ = os.Remove(pendingPath())
	rollbackReport.Store(true)
	armRollbackTimeout()
	log.Printf("upgrade: restored %s after %d failed starts; will report rollback then restart", p.Exe, p.Attempts)
	return true
}

func armRollbackTimeout() {
	gen := rollbackGen.Add(1)
	wait := rollbackHelloWait
	rollbackTimer = time.AfterFunc(wait, func() {
		if rollbackGen.Load() != gen || !rollbackReport.Load() {
			return
		}
		log.Printf("upgrade: rollback hello timed out, restarting into previous binary")
		restartAfterRollback()
	})
}

// upgradeRolledBack reports whether this process restored a previous binary
// and still owes the panel a hello.
func upgradeRolledBack() bool {
	if rollbackReport.Load() {
		return true
	}
	_, err := os.Stat(markerPath())
	return err == nil
}

// upgradePending reports whether a replaced binary has not yet hello'd.
func upgradePendingFile() bool {
	p, err := readPending()
	return err == nil && p != nil
}

// NoteUpgradeHealthy runs after hello_ack. A rollback report restarts into
// the restored binary. Any other successful hello clears the pending file and
// the backup so a later panel outage cannot roll a healthy binary back.
func NoteUpgradeHealthy() {
	if upgradeStaged.Load() {
		return
	}
	if rollbackReport.Load() || markerPresent() {
		log.Printf("upgrade: reported rollback, restarting into previous binary")
		restartAfterRollback()
		return
	}
	clearUpgradeArtifacts()
}

func markerPresent() bool {
	_, err := os.Stat(markerPath())
	return err == nil
}

func restartAfterRollback() {
	rollbackRestartOnce.Do(func() {
		rollbackGen.Add(1)
		rollbackReport.Store(false)
		clearUpgradeArtifacts()
		restartFn()
	})
}

// RestartForRollback is the nft-probe failure path: the new binary cannot
// even start, so restart into the copy already restored on disk.
func RestartForRollback() {
	restartAfterRollback()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".rollback-tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
