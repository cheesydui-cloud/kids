package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Backup writes a consistent, compacted snapshot of the database to destPath
// using SQLite's VACUUM INTO. It captures a correct snapshot even under WAL and
// in a single file, so recovery is just copying the file back over panel.db.
// destPath must not already exist (VACUUM INTO refuses to overwrite).
func Backup(d *sql.DB, destPath string) error {
	// destPath is operator-controlled (derived from --db), not user input, but
	// escape quotes anyway since VACUUM INTO takes the path inline, not as a bind.
	esc := strings.ReplaceAll(destPath, "'", "''")
	if _, err := d.Exec("VACUUM INTO '" + esc + "'"); err != nil {
		return fmt.Errorf("vacuum into %s: %w", destPath, err)
	}
	if err := ValidateBackup(destPath); err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("validate backup %s: %w", destPath, err)
	}
	return nil
}

// ValidateBackup opens a snapshot read-only and runs SQLite's integrity_check.
// It is intentionally separate so export/import callers can validate an
// archive snapshot without modifying the live database.
func ValidateBackup(path string) error {
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return err
	}
	defer d.Close()
	var result string
	if err := d.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity_check: %s", result)
	}
	return nil
}

// backupRun is the most recent automatic backup attempt in this process.
var backupRun struct {
	mu       sync.Mutex
	dir      string
	keep     int
	interval time.Duration
	lastErr  string
	lastFile string
	lastAt   time.Time
}

func noteBackupConfig(dir string, keep int, interval time.Duration) {
	backupRun.mu.Lock()
	backupRun.dir = dir
	backupRun.keep = keep
	backupRun.interval = interval
	backupRun.mu.Unlock()
}

func noteBackup(dir string, keep int, interval time.Duration, file string, err error) {
	backupRun.mu.Lock()
	defer backupRun.mu.Unlock()
	backupRun.dir = dir
	backupRun.keep = keep
	backupRun.interval = interval
	if err != nil {
		backupRun.lastErr = err.Error()
		return
	}
	backupRun.lastErr = ""
	backupRun.lastFile = file
	backupRun.lastAt = time.Now()
}

// BackupStatus describes local panel-*.db snapshots beside the database.
type BackupStatus struct {
	Dir             string `json:"dir"`
	Count           int    `json:"count"`
	LatestName      string `json:"latest_name"`
	LatestUnix      int64  `json:"latest_unix"`
	LastError       string `json:"last_error"`
	Keep            int    `json:"keep"`
	IntervalSeconds int    `json:"interval_seconds"`
}

// ReadBackupStatus lists snapshots in dir and overlays the latest in-process
// error. dir is typically <data-dir>/backups.
func ReadBackupStatus(dir string) BackupStatus {
	st := BackupStatus{Dir: dir}
	backupRun.mu.Lock()
	if backupRun.dir == dir || backupRun.dir == "" {
		st.Keep = backupRun.keep
		st.IntervalSeconds = int(backupRun.interval / time.Second)
		st.LastError = backupRun.lastErr
	}
	backupRun.mu.Unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return st
		}
		if st.LastError == "" {
			st.LastError = err.Error()
		}
		return st
	}
	var newest string
	var newestMod time.Time
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "panel-") || !strings.HasSuffix(name, ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		st.Count++
		if newest == "" || info.ModTime().After(newestMod) {
			newest = name
			newestMod = info.ModTime()
		}
	}
	st.LatestName = newest
	if !newestMod.IsZero() {
		st.LatestUnix = newestMod.Unix()
	}
	return st
}

// StartBackups runs a periodic local backup of the panel DB into a "backups"
// directory next to it, retaining the most recent `keep` snapshots. It takes one
// backup immediately, then every `interval`. interval<=0 or keep<=0 disables it.
// The returned func stops the loop. Backups live on the same host/disk, so they
// guard against DB corruption, a bad migration or accidental deletion — offsite
// copies remain the operator's job.
func StartBackups(d *sql.DB, dbPath string, interval time.Duration, keep int) func() {
	if interval <= 0 || keep <= 0 {
		return func() {}
	}
	dir := filepath.Join(filepath.Dir(dbPath), "backups")
	noteBackupConfig(dir, keep, interval)
	stop := make(chan struct{})
	run := func() {
		if err := ensureDir(dir); err != nil {
			noteBackup(dir, keep, interval, "", err)
			log.Printf("backup: ensure dir %s: %v", dir, err)
			return
		}
		dest := filepath.Join(dir, "panel-"+time.Now().Format("20060102-150405")+".db")
		// VACUUM INTO refuses to overwrite. A backup for this second already
		// existing means a near-simultaneous run (e.g. two processes overlapping
		// during an upgrade restart) already captured it — skip rather than log a
		// spurious error.
		if _, err := os.Stat(dest); err == nil {
			return
		}
		if err := Backup(d, dest); err != nil {
			noteBackup(dir, keep, interval, "", err)
			log.Printf("backup: %v", err)
			return
		}
		noteBackup(dir, keep, interval, filepath.Base(dest), nil)
		pruneBackups(dir, keep)
	}
	go func() {
		run()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				run()
			}
		}
	}()
	return func() { close(stop) }
}

// pruneBackups keeps only the newest `keep` panel-*.db files in dir. Names embed
// a sortable timestamp, so lexical order is chronological.
func pruneBackups(dir string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "panel-") && strings.HasSuffix(e.Name(), ".db") {
			files = append(files, e.Name())
		}
	}
	if len(files) <= keep {
		return
	}
	sort.Strings(files)
	for _, name := range files[:len(files)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			log.Printf("backup: prune %s: %v", name, err)
		}
	}
}
