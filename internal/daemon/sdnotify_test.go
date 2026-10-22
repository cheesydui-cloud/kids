package daemon

import (
	"os"
	"testing"
	"time"
)

func TestSystemdWatchdogIntervalEmpty(t *testing.T) {
	t.Setenv("WATCHDOG_USEC", "")
	if got := systemdWatchdogInterval(); got != 0 {
		t.Fatalf("want 0, got %v", got)
	}
}

func TestSystemdWatchdogIntervalHalf(t *testing.T) {
	// 60s watchdog → 30s ping interval.
	t.Setenv("WATCHDOG_USEC", "60000000")
	got := systemdWatchdogInterval()
	want := 30 * time.Second
	if got != want {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestWriteRestartUnlimitedDropIn(t *testing.T) {
	dir := t.TempDir()
	unit := dir + "/nft-daemon.service"
	drop := dir + "/d/restart-unlimited.conf"
	changed, err := writeRestartUnlimitedDropIn(unit, drop)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("missing unit should not write a drop-in")
	}
	if err := os.WriteFile(unit, []byte("StartLimitBurst=8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = writeRestartUnlimitedDropIn(unit, drop)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected drop-in to be written")
	}
	got, err := os.ReadFile(drop)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != restartUnlimitedDropIn {
		t.Fatalf("drop-in = %q", got)
	}
	changed, err = writeRestartUnlimitedDropIn(unit, drop)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("identical drop-in should not be rewritten")
	}
}

func TestProgressStalled(t *testing.T) {
	dl := NewDialer(DialerConfig{})
	if dl.progressStalled(time.Now()) {
		t.Fatal("zero progress is not stalled")
	}
	dl.stallAfter = time.Second
	dl.progress.Store(time.Now().Add(-2 * time.Second).UnixNano())
	if !dl.progressStalled(time.Now()) {
		t.Fatal("expected stall")
	}
	dl.touchProgress()
	if dl.progressStalled(time.Now()) {
		t.Fatal("fresh progress is not stalled")
	}
}

func TestSdNotifyNoSocketIsNoop(t *testing.T) {
	os.Unsetenv("NOTIFY_SOCKET")
	// Must not panic when unsupervised.
	sdNotify("READY=1")
	sdNotify("WATCHDOG=1")
}
