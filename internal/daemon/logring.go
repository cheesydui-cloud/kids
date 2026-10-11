package daemon

import (
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

const (
	logRingCap = 80
	logRingMax = 240
)

// agentLogs keeps a short filtered tail of log.Printf for the panel pull.
// The full journal stays on the node; this ring is not a log shipper.
var agentLogs struct {
	mu    sync.Mutex
	lines []string
}

type ringWriter struct{}

func (ringWriter) Write(p []byte) (int, error) {
	s := trimLogLine(string(p))
	if s == "" || !logInteresting(s) {
		return len(p), nil
	}
	agentLogs.mu.Lock()
	agentLogs.lines = append(agentLogs.lines, s)
	if len(agentLogs.lines) > logRingCap {
		agentLogs.lines = append([]string(nil), agentLogs.lines[len(agentLogs.lines)-logRingCap:]...)
	}
	agentLogs.mu.Unlock()
	return len(p), nil
}

func trimLogLine(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > logRingMax {
		return string(r[:logRingMax])
	}
	return s
}

func logInteresting(s string) bool {
	for _, k := range []string{"upgrade:", "dialer:", "reconcile:", "counters:", "dns refresh", "resource:", "rollback"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

var logRingOnce sync.Once

// InstallLogRing tees log.Printf to stderr and the pull ring. Call once at
// daemon start, before the first operational log line.
func InstallLogRing() {
	logRingOnce.Do(func() {
		log.SetOutput(io.MultiWriter(os.Stderr, ringWriter{}))
	})
}

// RecentLogs returns a copy of the filtered ring, oldest first.
func RecentLogs() []string {
	agentLogs.mu.Lock()
	defer agentLogs.mu.Unlock()
	if len(agentLogs.lines) == 0 {
		return []string{}
	}
	out := make([]string, len(agentLogs.lines))
	copy(out, agentLogs.lines)
	return out
}

// resetLogRingForTest clears the ring. Tests that share the process must
// call it before asserting contents.
func resetLogRingForTest() {
	agentLogs.mu.Lock()
	agentLogs.lines = nil
	agentLogs.mu.Unlock()
}
