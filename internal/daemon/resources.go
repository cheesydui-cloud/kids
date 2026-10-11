package daemon

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"syscall"

	"nft/internal/forward"
)

const nofileTarget = 1048576

// RaiseNOFILE lifts RLIMIT_NOFILE at daemon start. The systemd unit sets the
// same ceiling for new installs; this covers a unit that was written before
// that line. Failure is logged and ignored — a low limit must not stop boot.
func RaiseNOFILE() {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		log.Printf("resource: getrlimit: %v", err)
		return
	}
	want := uint64(nofileTarget)
	if lim.Cur >= want && lim.Max >= want {
		return
	}
	lim.Cur = want
	lim.Max = want
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		var cur syscall.Rlimit
		if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &cur) == nil && cur.Cur < cur.Max {
			cur.Cur = cur.Max
			if err2 := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &cur); err2 != nil {
				log.Printf("resource: setrlimit nofile: %v", err)
			}
		} else {
			log.Printf("resource: setrlimit nofile: %v", err)
		}
	}
}

// resourceSample is the watermark input. Tests build it directly.
type resourceSample struct {
	ConntrackCount int64
	ConntrackMax   int64
	FDCount        int64
	FDLimit        int64
	RelayHotPort   int
	RelayHotCount  int64
	RelayCap       int64
}

// resourceWarningFrom returns a Chinese warning when any sample is at or
// above 80% of its ceiling. Empty means the watermark is clear.
func resourceWarningFrom(s resourceSample) string {
	var parts []string
	if s.ConntrackMax > 0 && s.ConntrackCount*100 >= s.ConntrackMax*80 {
		parts = append(parts, fmt.Sprintf("连接跟踪已用 %d%%（%d/%d）", pct(s.ConntrackCount, s.ConntrackMax), s.ConntrackCount, s.ConntrackMax))
	}
	if s.FDLimit > 0 && s.FDCount*100 >= s.FDLimit*80 {
		parts = append(parts, fmt.Sprintf("文件描述符已用 %d%%（%d/%d）", pct(s.FDCount, s.FDLimit), s.FDCount, s.FDLimit))
	}
	if s.RelayCap > 0 && s.RelayHotCount*100 >= s.RelayCap*80 {
		parts = append(parts, fmt.Sprintf("用户态转发连接接近上限（端口 %d 已用 %d/%d）", s.RelayHotPort, s.RelayHotCount, s.RelayCap))
	}
	return strings.Join(parts, "；")
}

func pct(used, max int64) int64 {
	if max <= 0 {
		return 0
	}
	return used * 100 / max
}

func resourceWarning() string {
	return resourceWarningFrom(sampleResources())
}

func sampleResources() resourceSample {
	s := resourceSample{RelayCap: int64(forward.UserspaceConnCap())}
	if c, m, ok := readConntrack(); ok {
		s.ConntrackCount, s.ConntrackMax = c, m
	}
	s.FDCount, s.FDLimit = fdUsage()
	_, port, hot := forward.RelayPressure()
	s.RelayHotPort, s.RelayHotCount = port, hot
	return s
}

func readConntrack() (count, max int64, ok bool) {
	cb, err1 := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_count")
	mb, err2 := os.ReadFile("/proc/sys/net/netfilter/nf_conntrack_max")
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	count, err1 = strconv.ParseInt(strings.TrimSpace(string(cb)), 10, 64)
	max, err2 = strconv.ParseInt(strings.TrimSpace(string(mb)), 10, 64)
	if err1 != nil || err2 != nil || max <= 0 {
		return 0, 0, false
	}
	return count, max, true
}

func fdUsage() (count, limit int64) {
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		count = int64(len(entries))
	}
	var lim syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim) == nil {
		limit = int64(lim.Cur)
	}
	return count, limit
}

func conntrackText() string {
	c, m, ok := readConntrack()
	if !ok {
		return "不可用"
	}
	return fmt.Sprintf("%d/%d", c, m)
}

func fdText() string {
	c, lim := fdUsage()
	if lim <= 0 {
		return fmt.Sprintf("%d", c)
	}
	return fmt.Sprintf("%d/%d", c, lim)
}
