package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nft/internal/wsproto"
)

func (d *Dialer) handleUpgrade(ctx context.Context, u wsproto.Upgrade) wsproto.UpgradeAck {
	log.Printf("upgrade: received version=%s sha256=%s size=%d data=%d from=%s",
		u.Version, u.SHA256, u.Size, len(u.Data), u.DownloadAt)

	// Label-only sync: the panel sends no binary when the running agent's sha
	// already matches the target. Confirm against our own binary (the panel's
	// view may be stale) and, if it holds, just record the new version label —
	// no replace, no restart. On mismatch reject so the panel re-pushes the
	// bytes.
	if len(u.Data) == 0 && u.DownloadAt == "" {
		if u.SHA256 != "" && u.SHA256 == agentSHA() {
			writeAgentIdentity(u.Version, u.SHA256)
			log.Printf("upgrade: already on sha %s, recorded version=%s — restarting to pick up label", u.SHA256, u.Version)
			go restartSelf()
			return wsproto.UpgradeAck{OK: true}
		}
		return wsproto.UpgradeAck{Error: "binary sha mismatch; full push required"}
	}

	binary, err := upgradeBinary(u)
	if err != nil {
		log.Printf("upgrade: obtain binary failed: %v", err)
		return wsproto.UpgradeAck{Error: err.Error()}
	}

	exePath, err := os.Executable()
	if err != nil {
		return wsproto.UpgradeAck{Error: "os.Executable: " + err.Error()}
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return wsproto.UpgradeAck{Error: "resolve symlink: " + err.Error()}
	}

	if err := atomicReplace(exePath, binary); err != nil {
		return wsproto.UpgradeAck{Error: "replace binary: " + err.Error()}
	}
	// Persist the new identity before restart so the relaunched (reproducible)
	// binary reports the right version label, which it cannot self-derive.
	writeAgentIdentity(u.Version, u.SHA256)
	log.Printf("upgrade: binary replaced at %s (%d bytes), scheduling restart", exePath, len(binary))

	go restartSelf()

	return wsproto.UpgradeAck{OK: true}
}

// upgradeBinary returns the new binary for u: the inline Data (sha-verified)
// when present, otherwise an HTTP download from u.DownloadAt. Inline transport
// lets nodes that cannot reach the panel over HTTP still upgrade over the WS
// link; the download path stays for daemons reached by an older panel.
func upgradeBinary(u wsproto.Upgrade) ([]byte, error) {
	if len(u.Data) > 0 {
		sum := sha256.Sum256(u.Data)
		if got := hex.EncodeToString(sum[:]); got != u.SHA256 {
			return nil, fmt.Errorf("sha256 mismatch: got %s, want %s", got, u.SHA256)
		}
		return u.Data, nil
	}
	return downloadBinary(u)
}

// fixUpgradeDownloadURL repairs bare host:port download URLs from older panels
// that omitted the scheme (e.g. "1.2.3.4:7788/v1/binary"), which net/http
// rejects with "first path segment in URL cannot contain colon".
func fixUpgradeDownloadURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	return "http://" + raw
}

func downloadBinary(u wsproto.Upgrade) ([]byte, error) {
	// Domestic reverse links often stall mid-file. Resume with Range and retry
	// instead of one 3-minute all-or-nothing GET that dies with the control WS.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	url := fixUpgradeDownloadURL(u.DownloadAt)
	client := &http.Client{Timeout: 90 * time.Second}
	want := u.Size
	if want < 1 {
		want = 64 << 20
	}
	var buf []byte
	var lastErr error
	for attempt := 1; attempt <= 8; attempt++ {
		if ctx.Err() != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "nft-agent-upgrade")
		if len(buf) > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(buf)))
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("GET %s: %w", url, err)
			log.Printf("upgrade: download attempt %d failed: %v", attempt, lastErr)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		chunk, readErr := io.ReadAll(io.LimitReader(resp.Body, want+1024-int64(len(buf))))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			buf = chunk
		} else if resp.StatusCode == http.StatusPartialContent && len(buf) > 0 {
			buf = append(buf, chunk...)
		} else {
			lastErr = fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
			log.Printf("upgrade: download attempt %d: %v", attempt, lastErr)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		if readErr != nil || (u.Size > 0 && int64(len(buf)) < u.Size) {
			if readErr != nil {
				lastErr = fmt.Errorf("read body: %w", readErr)
			} else {
				lastErr = fmt.Errorf("GET %s: short body %d < %d", url, len(buf), u.Size)
			}
			log.Printf("upgrade: download attempt %d short (%d bytes): %v", attempt, len(buf), lastErr)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		log.Printf("upgrade: downloaded %d bytes from %s", len(buf), url)
		h := sha256.Sum256(buf)
		got := hex.EncodeToString(h[:])
		if got != u.SHA256 {
			return nil, fmt.Errorf("sha256 mismatch: got %s, want %s", got, u.SHA256)
		}
		return buf, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("GET %s: exhausted retries", url)
	}
	return nil, lastErr
}

func atomicReplace(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".nft-upgrade-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0755); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func restartSelf() {
	time.Sleep(time.Second)
	unit := detectUnit()
	log.Printf("upgrade: restarting unit %s", unit)
	if out, err := exec.Command(
		"systemd-run", "--no-block", "--",
		"systemctl", "restart", unit,
	).CombinedOutput(); err != nil {
		log.Printf("upgrade: systemd-run restart failed: %v: %s — trying direct restart", err, out)
		exec.Command("systemctl", "restart", unit).Start()
	}
	// Binary is already replaced. Exit so systemd Restart=always relaunches
	// even if we guessed the unit name wrong (nft vs nft-daemon vs nft-agent).
	time.Sleep(2 * time.Second)
	os.Exit(0)
}

func detectUnit() string {
	pid := os.Getpid()
	out, err := exec.Command("systemctl", "status", fmt.Sprintf("%d", pid), "--no-pager", "-l").CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, ".service") {
				fields := strings.Fields(line)
				for _, f := range fields {
					f = strings.TrimSuffix(f, ":")
					if strings.HasPrefix(f, "nft") && strings.HasSuffix(f, ".service") {
						return strings.TrimSuffix(f, ".service")
					}
				}
			}
		}
	}
	for _, name := range []string{"nft-daemon", "nft-agent", "nft"} {
		if out, err := exec.Command("systemctl", "is-active", name+".service").CombinedOutput(); err == nil && strings.TrimSpace(string(out)) == "active" {
			return name
		}
	}
	return "nft-daemon"
}
