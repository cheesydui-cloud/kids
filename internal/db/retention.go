package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

// RetentionPolicy controls only historical operational data. A value <= 0
// disables that class of cleanup. Core business tables and cumulative traffic
// ledgers are deliberately outside this policy.
type RetentionPolicy struct {
	AuditDays         int
	HourlyTrafficDays int
	DailyTrafficDays  int
}

// CleanupHistory deletes rows older than the configured calendar cutoffs.
// Traffic day keys use Asia/Shanghai; hourly keys are YYYY-MM-DDTHH in the
// same location. The operation is idempotent and safe to run periodically.
func CleanupHistory(d *sql.DB, p RetentionPolicy, now time.Time) error {
	if d == nil {
		return fmt.Errorf("nil database")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if p.AuditDays > 0 {
		cutoff := now.Unix() - int64(p.AuditDays)*86400
		if _, err := tx.Exec(`DELETE FROM audit_logs WHERE at < ?`, cutoff); err != nil {
			return fmt.Errorf("cleanup audit logs: %w", err)
		}
	}
	if p.HourlyTrafficDays > 0 {
		cutoff := hourKey(now.Add(-time.Duration(p.HourlyTrafficDays) * 24 * time.Hour))
		if _, err := tx.Exec(`DELETE FROM hourly_raw_traffic WHERE hour < ?`, cutoff); err != nil {
			return fmt.Errorf("cleanup hourly traffic: %w", err)
		}
	}
	if p.DailyTrafficDays > 0 {
		cutoff := dayKey(now.Add(-time.Duration(p.DailyTrafficDays) * 24 * time.Hour))
		if _, err := tx.Exec(`DELETE FROM daily_user_traffic WHERE day < ?`, cutoff); err != nil {
			return fmt.Errorf("cleanup user traffic: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM daily_node_raw_traffic WHERE day < ?`, cutoff); err != nil {
			return fmt.Errorf("cleanup node traffic: %w", err)
		}
	}
	return tx.Commit()
}

// StartHistoryCleanup runs one cleanup immediately and then once per day.
// The returned function is safe for the server shutdown path.
func StartHistoryCleanup(d *sql.DB, p RetentionPolicy) func() {
	if p.AuditDays <= 0 && p.HourlyTrafficDays <= 0 && p.DailyTrafficDays <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	run := func() {
		if err := CleanupHistory(d, p, time.Now()); err != nil {
			log.Printf("retention: %v", err)
		}
	}
	go func() {
		run()
		t := time.NewTicker(24 * time.Hour)
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
