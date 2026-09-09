package db

import (
	"testing"
	"time"
)

func TestCleanupHistoryOnlyRemovesExpiredOperationalRows(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	nid := createTestNode(t, d, "retention-node")
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, panelBusinessLocation)
	oldUnix := now.Add(-200 * 24 * time.Hour).Unix()
	oldDay := dayKey(now.Add(-800 * 24 * time.Hour))
	keepDay := dayKey(now.Add(-700 * 24 * time.Hour))
	oldHour := hourKey(now.Add(-40 * 24 * time.Hour))
	keepHour := hourKey(now.Add(-20 * 24 * time.Hour))

	if _, err := d.Exec(`INSERT INTO audit_logs(user_id, action, target, payload, at) VALUES (?,?,?,?,?)`, uid, "old", "x", "", oldUnix); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO audit_logs(user_id, action, target, payload, at) VALUES (?,?,?,?,?)`, uid, "keep", "x", "", now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO hourly_raw_traffic(hour, raw_bytes) VALUES (?,?)`, oldHour, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO hourly_raw_traffic(hour, raw_bytes) VALUES (?,?)`, keepHour, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO daily_user_traffic(day, user_id, raw_bytes) VALUES (?,?,?)`, oldDay, uid, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO daily_user_traffic(day, user_id, raw_bytes) VALUES (?,?,?)`, keepDay, uid, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO daily_node_raw_traffic(day, node_id, raw_bytes) VALUES (?,?,?)`, oldDay, nid, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO daily_node_raw_traffic(day, node_id, raw_bytes) VALUES (?,?,?)`, keepDay, nid, 6); err != nil {
		t.Fatal(err)
	}

	if err := CleanupHistory(d, RetentionPolicy{AuditDays: 180, HourlyTrafficDays: 30, DailyTrafficDays: 730}, now); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action="old"`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old audit row remains: %d", count)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action="keep"`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new audit row count = %d, want 1", count)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM hourly_raw_traffic WHERE hour=?`, oldHour).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old hourly row remains: %d", count)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM hourly_raw_traffic WHERE hour=?`, keepHour).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new hourly row count = %d, want 1", count)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM daily_user_traffic WHERE day=?`, oldDay).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old user-day row remains: %d", count)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM daily_user_traffic WHERE day=?`, keepDay).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new user-day row count = %d, want 1", count)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM daily_node_raw_traffic WHERE day=?`, oldDay).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old node-day row remains: %d", count)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM daily_node_raw_traffic WHERE day=?`, keepDay).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new node-day row count = %d, want 1", count)
	}

	var username string
	if err := d.QueryRow(`SELECT username FROM users WHERE id=?`, uid).Scan(&username); err != nil || username == "" {
		t.Fatalf("core user row changed: %v %q", err, username)
	}
}

func TestCleanupHistoryCanBeDisabled(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.Exec(`INSERT INTO audit_logs(user_id, action, target, payload, at) VALUES (?,?,?,?,?)`, nil, "keep", "x", "", 1); err != nil {
		t.Fatal(err)
	}
	if err := CleanupHistory(d, RetentionPolicy{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action="keep"`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("disabled cleanup removed row: %d", count)
	}
}
