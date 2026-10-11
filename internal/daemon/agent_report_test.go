package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShouldPersistDisconnectSkipsCleanStop(t *testing.T) {
	if shouldPersistDisconnect("agent_stop") || shouldPersistDisconnect("context_canceled") || shouldPersistDisconnect("") {
		t.Fatal("clean stop must not overwrite the stored reason")
	}
	if !shouldPersistDisconnect("watchdog_stale") {
		t.Fatal("watchdog_stale should be stored")
	}
}

func TestDisconnectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "disconnect.json")
	saveDisconnect(path, "watchdog_stale", 1700000000)
	reason, at := loadDisconnect(path)
	if reason != "watchdog_stale" || at != 1700000000 {
		t.Fatalf("got %q %d", reason, at)
	}
	saveDisconnect(path, "agent_stop", 1700000001)
	reason, at = loadDisconnect(path)
	if reason != "watchdog_stale" || at != 1700000000 {
		t.Fatalf("clean stop overwrote reason: %q %d", reason, at)
	}
}

func TestDisconnectReasonLabel(t *testing.T) {
	if DisconnectReasonLabel("watchdog_stale") != "看门狗超时" {
		t.Fatal(DisconnectReasonLabel("watchdog_stale"))
	}
	if DisconnectReasonLabel("dial_or_hello_timeout") != "握手超时" {
		t.Fatal(DisconnectReasonLabel("dial_or_hello_timeout"))
	}
}

func TestResourceWarningAt80(t *testing.T) {
	quiet := resourceWarningFrom(resourceSample{ConntrackCount: 79, ConntrackMax: 100, FDCount: 79, FDLimit: 100, RelayHotCount: 79, RelayCap: 100, RelayHotPort: 1})
	if quiet != "" {
		t.Fatalf("79%% should be quiet, got %q", quiet)
	}
	got := resourceWarningFrom(resourceSample{ConntrackCount: 80, ConntrackMax: 100})
	if got == "" || !strings.Contains(got, "连接跟踪已用 80%") {
		t.Fatalf("conntrack: %q", got)
	}
	got = resourceWarningFrom(resourceSample{FDCount: 80, FDLimit: 100})
	if got == "" || !strings.Contains(got, "文件描述符已用 80%") {
		t.Fatalf("fd: %q", got)
	}
	got = resourceWarningFrom(resourceSample{RelayHotPort: 20000, RelayHotCount: 80, RelayCap: 100})
	if got == "" || !strings.Contains(got, "端口 20000") {
		t.Fatalf("relay: %q", got)
	}
}

func TestLogRingCapAndFilter(t *testing.T) {
	resetLogRingForTest()
	t.Cleanup(resetLogRingForTest)
	if _, err := (ringWriter{}).Write([]byte("dialer: reconnect reason=watchdog_stale\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := (ringWriter{}).Write([]byte("listening on socket\n")); err != nil {
		t.Fatal(err)
	}
	if got := RecentLogs(); len(got) != 1 {
		t.Fatalf("filter: %#v", got)
	}
	long := "upgrade: " + string(make([]byte, 400))
	if _, err := (ringWriter{}).Write([]byte(long)); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(RecentLogs()[1])); n > logRingMax {
		t.Fatalf("line len %d", n)
	}
	resetLogRingForTest()
	for i := 0; i < 100; i++ {
		_, _ = (ringWriter{}).Write([]byte("upgrade: step\n"))
	}
	if n := len(RecentLogs()); n != logRingCap {
		t.Fatalf("cap %d", n)
	}
}

func TestRollbackRestoresOnThirdStartWithoutExit(t *testing.T) {
	dir := t.TempDir()
	SetUpgradeDir(dir)
	restarted := 0
	prev := restartFn
	restartFn = func() { restarted++ }
	t.Cleanup(func() {
		ResetUpgradeStateForTest()
		restartFn = prev
		SetUpgradeDir("/var/lib/nft")
	})
	exe := filepath.Join(dir, "nft-agent")
	if err := os.WriteFile(exe, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	bak := exe + ".prev"
	if err := os.WriteFile(bak, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePending(upgradePending{Exe: exe, Bak: bak, Attempts: 0, Version: "v0.10.0"}); err != nil {
		t.Fatal(err)
	}

	if RollbackIfUpgradeUnhealthy() {
		t.Fatal("attempt 1 must not roll back")
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new-binary" {
		t.Fatalf("exe after attempt 1: %s %v", got, err)
	}
	if RollbackIfUpgradeUnhealthy() {
		t.Fatal("attempt 2 must not roll back")
	}
	if !RollbackIfUpgradeUnhealthy() {
		t.Fatal("attempt 3 must roll back")
	}
	got, err = os.ReadFile(exe)
	if err != nil || string(got) != "old-binary" {
		t.Fatalf("exe after rollback: %s %v", got, err)
	}
	if restarted != 0 {
		t.Fatal("rollback must keep the new process running until hello")
	}
	if !upgradeRolledBack() {
		t.Fatal("hello should report upgrade_rolled_back")
	}
	NoteUpgradeHealthy()
	if restarted != 1 {
		t.Fatalf("restart calls = %d, want 1", restarted)
	}
	if _, err := os.Stat(filepath.Join(dir, upgradePendingName)); !os.IsNotExist(err) {
		t.Fatal("pending file should be gone after the report")
	}
}

func TestRollbackTimeoutRestartsWithoutHello(t *testing.T) {
	dir := t.TempDir()
	SetUpgradeDir(dir)
	prevWait := rollbackHelloWait
	rollbackHelloWait = 30 * time.Millisecond
	restarted := 0
	prev := restartFn
	restartFn = func() { restarted++ }
	t.Cleanup(func() {
		ResetUpgradeStateForTest()
		restartFn = prev
		rollbackHelloWait = prevWait
		SetUpgradeDir("/var/lib/nft")
	})
	exe := filepath.Join(dir, "nft-agent")
	bak := exe + ".prev"
	_ = os.WriteFile(exe, []byte("new"), 0o755)
	_ = os.WriteFile(bak, []byte("old"), 0o755)
	_ = writePending(upgradePending{Exe: exe, Bak: bak, Attempts: 2})
	if !RollbackIfUpgradeUnhealthy() {
		t.Fatal("want rollback")
	}
	deadline := time.Now().Add(time.Second)
	for restarted == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if restarted != 1 {
		t.Fatal("hello deadline should restart into the restored binary")
	}
}
