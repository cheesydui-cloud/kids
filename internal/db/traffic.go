package db

import (
	"database/sql"
	"strings"
	"time"
)

// AddUserNodeTraffic increments the per-grant traffic counter for a user/node pair.
func AddUserNodeTraffic(d *sql.DB, userID, nodeID, delta int64) error {
	_, err := d.Exec(`UPDATE user_nodes SET traffic_used_bytes = traffic_used_bytes + ? WHERE user_id=? AND node_id=?`,
		delta, userID, nodeID)
	return err
}

// ResetUserNodeTraffic zeroes the traffic counter for a single user/node grant.
func ResetUserNodeTraffic(d *sql.DB, userID, nodeID int64) error {
	_, err := d.Exec(`UPDATE user_nodes SET traffic_used_bytes = 0 WHERE user_id=? AND node_id=?`, userID, nodeID)
	return err
}

// ResetAllUserTraffic zeroes the global traffic counter, all per-node counters,
// the landing-exit ledger and the displayed per-rule hop totals for a user —
// an admin reset promises a clean slate, so every number shown for the user
// must drop to zero together. rule_hops.last_bytes* are deliberately kept:
// they snapshot the agent's cumulative counters for delta computation, and
// zeroing them would re-bill the full counter value on the next sample.
func ResetAllUserTraffic(d *sql.DB, userID int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET traffic_used_bytes = 0, total_traffic_used_bytes = 0 WHERE id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE user_nodes SET traffic_used_bytes = 0 WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE user_landing_exits SET used_bytes = 0 WHERE user_id=?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE rule_hops SET total_bytes = 0, billed_bytes = 0 WHERE rule_id IN (SELECT id FROM rules WHERE owner_id = ?)`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// NodeTrafficSums returns total traffic_used_bytes per node across all users.
func NodeTrafficSums(d *sql.DB) (map[int64]int64, error) {
	rows, err := d.Query(`SELECT node_id, SUM(traffic_used_bytes) FROM user_nodes GROUP BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[int64]int64)
	for rows.Next() {
		var nodeID, total int64
		if err := rows.Scan(&nodeID, &total); err != nil {
			return nil, err
		}
		m[nodeID] = total
	}
	return m, rows.Err()
}

// AddNodeRawTraffic folds delta raw bytes into the node's cumulative ledger.
// Raw bytes are the node's real forwarded volume (uplink + downlink), kept
// apart from billing counters so multipliers, unidirectional billing and user
// resets never distort them.
func AddNodeRawTraffic(d DBTX, nodeID, delta int64) error {
	_, err := d.Exec(`INSERT INTO node_raw_traffic(node_id, raw_bytes) VALUES(?,?)
		ON CONFLICT(node_id) DO UPDATE SET raw_bytes = raw_bytes + excluded.raw_bytes`,
		nodeID, delta)
	return err
}

// panelBusinessLocation is the calendar used for "当日流量" buckets.
// Operator dashboards are China-facing; using the host's local TZ (often UTC
// on cloud VMs) made China early-morning still land in the previous UTC day,
// so the card kept showing yesterday's volume past 北京时间 0:00.
//
// Fixed to Asia/Shanghai rather than process Local. A future panel_timezone
// setting can override this if multi-region operators need it.
var panelBusinessLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// tzdata missing on some minimal images — fall back to fixed UTC+8
		// so the day boundary still matches 北京时间 even without zoneinfo.
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// dayKey returns the business calendar day as YYYY-MM-DD for daily traffic
// buckets (Asia/Shanghai, not the host's Local timezone).
func dayKey(t time.Time) string {
	return t.In(panelBusinessLocation).Format("2006-01-02")
}

// DayKeyNow is the Asia/Shanghai YYYY-MM-DD for "today" (北京时间切日).
func DayKeyNow() string {
	return dayKey(time.Now())
}

// ParseBusinessDateEnd parses a YYYY-MM-DD account-expiry date in Asia/Shanghai
// and returns the unix timestamp of that calendar day's last second (23:59:59).
// time.Parse("2006-01-02") would store UTC midnight, which is 北京时间 08:00 and
// cuts the purchased day short.
func ParseBusinessDateEnd(raw string) (int64, error) {
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), panelBusinessLocation)
	if err != nil {
		return 0, err
	}
	end := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, panelBusinessLocation)
	return end.Unix(), nil
}

// AddNodeDailyRawTraffic folds delta raw bytes into today's per-node ledger.
// Same actual-traffic semantics as AddNodeRawTraffic (no billing multiplier).
// "Today" is the Asia/Shanghai calendar day.
func AddNodeDailyRawTraffic(d DBTX, nodeID, delta int64) error {
	if delta == 0 {
		return nil
	}
	day := dayKey(time.Now())
	_, err := d.Exec(`INSERT INTO daily_node_raw_traffic(day, node_id, raw_bytes) VALUES(?,?,?)
		ON CONFLICT(day, node_id) DO UPDATE SET raw_bytes = raw_bytes + excluded.raw_bytes`,
		day, nodeID, delta)
	return err
}

// TodayRawTrafficBytes sums today's actual (raw) traffic across all nodes.
// "Today" is the Asia/Shanghai calendar day (北京时间 0 点切日).
func TodayRawTrafficBytes(d *sql.DB) (int64, error) {
	var total int64
	err := d.QueryRow(`SELECT COALESCE(SUM(raw_bytes),0) FROM daily_node_raw_traffic WHERE day=?`,
		dayKey(time.Now())).Scan(&total)
	return total, err
}

// hourKey returns the Asia/Shanghai hour bucket as YYYY-MM-DDTHH.
func hourKey(t time.Time) string {
	return t.In(panelBusinessLocation).Format("2006-01-02T15")
}

// HourKeyNow is the Asia/Shanghai hour bucket for "this hour".
func HourKeyNow() string {
	return hourKey(time.Now())
}

// AddHourlyRawTraffic folds delta raw bytes into the current hour bucket.
// Same actual-traffic semantics as AddNodeDailyRawTraffic (no billing multiplier).
func AddHourlyRawTraffic(d DBTX, delta int64) error {
	if delta == 0 {
		return nil
	}
	hour := hourKey(time.Now())
	_, err := d.Exec(`INSERT INTO hourly_raw_traffic(hour, raw_bytes) VALUES(?,?)
		ON CONFLICT(hour) DO UPDATE SET raw_bytes = raw_bytes + excluded.raw_bytes`,
		hour, delta)
	return err
}

// HourlyTrafficPoint is one hour on the 24h ops curve.
type HourlyTrafficPoint struct {
	Hour  string `json:"hour"`
	Bytes int64  `json:"bytes"`
}

// Last24hRawTraffic returns 24 consecutive Asia/Shanghai hour buckets ending
// at the current hour. Missing rows are 0 so the chart always has a full day.
func Last24hRawTraffic(d *sql.DB) ([]HourlyTrafficPoint, error) {
	now := time.Now().In(panelBusinessLocation)
	start := now.Add(-23 * time.Hour)
	start = time.Date(start.Year(), start.Month(), start.Day(), start.Hour(), 0, 0, 0, panelBusinessLocation)
	endHour := hourKey(now)

	rows, err := d.Query(`SELECT hour, raw_bytes FROM hourly_raw_traffic WHERE hour>=? AND hour<=?`,
		hourKey(start), endHour)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[string]int64{}
	for rows.Next() {
		var hour string
		var bytes int64
		if err := rows.Scan(&hour, &bytes); err != nil {
			return nil, err
		}
		got[hour] = bytes
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]HourlyTrafficPoint, 24)
	for i := 0; i < 24; i++ {
		t := start.Add(time.Duration(i) * time.Hour)
		k := hourKey(t)
		out[i] = HourlyTrafficPoint{Hour: k, Bytes: got[k]}
	}
	return out, nil
}

// AddUserDailyTraffic folds delta raw bytes into today's per-user ledger.
// Delta is the same raw final-hop volume that advances users.traffic_used_bytes
// (not rate-multiplied). "Today" is the Asia/Shanghai calendar day.
func AddUserDailyTraffic(d DBTX, userID, delta int64) error {
	if delta == 0 {
		return nil
	}
	day := dayKey(time.Now())
	_, err := d.Exec(`INSERT INTO daily_user_traffic(day, user_id, raw_bytes) VALUES(?,?,?)
		ON CONFLICT(day, user_id) DO UPDATE SET raw_bytes = raw_bytes + excluded.raw_bytes`,
		day, userID, delta)
	return err
}

// UserTrafficBytesOnDay returns one user's raw traffic for the given
// Asia/Shanghai YYYY-MM-DD day key. Missing rows are 0.
func UserTrafficBytesOnDay(d *sql.DB, userID int64, day string) (int64, error) {
	var total int64
	err := d.QueryRow(`SELECT COALESCE(raw_bytes,0) FROM daily_user_traffic WHERE day=? AND user_id=?`,
		day, userID).Scan(&total)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return total, err
}

// TodayUserTrafficBytes returns one user's raw traffic for the current
// Asia/Shanghai calendar day (北京时间 0 点切日). Missing rows are 0.
func TodayUserTrafficBytes(d *sql.DB, userID int64) (int64, error) {
	return UserTrafficBytesOnDay(d, userID, dayKey(time.Now()))
}

// monthKey returns the Asia/Shanghai calendar month as YYYY-MM.
func monthKey(t time.Time) string {
	return t.In(panelBusinessLocation).Format("2006-01")
}

// MonthKeyNow is the Asia/Shanghai YYYY-MM for the current month.
func MonthKeyNow() string {
	return monthKey(time.Now())
}

// TodayUserRawTrafficBytes sums today's last-hop raw traffic across users.
// This is actual usage (no billing_rate) and does not stack entry+exit hops
// the way daily_node_raw_traffic does. Built-in admin is excluded.
func TodayUserRawTrafficBytes(d *sql.DB) (int64, error) {
	var total int64
	err := d.QueryRow(`
		SELECT COALESCE(SUM(d.raw_bytes), 0)
		FROM daily_user_traffic d
		JOIN users u ON u.id = d.user_id
			WHERE d.day = ? AND u.username <> 'admin'`, dayKey(time.Now())).Scan(&total)
	return total, err
}

// MonthUserRawTrafficBytes sums this Asia/Shanghai calendar month's last-hop
// raw traffic across users. No billing_rate. The 1st at 00:00 北京时间 starts
// a new month prefix, so the previous month drops out without a wipe.
func MonthUserRawTrafficBytes(d *sql.DB) (int64, error) {
	var total int64
	err := d.QueryRow(`
			SELECT COALESCE(SUM(d.raw_bytes), 0)
			FROM daily_user_traffic d
			JOIN users u ON u.id = d.user_id
			WHERE d.day LIKE ? AND u.username <> 'admin'`, MonthKeyNow()+"%").Scan(&total)
	return total, err
}

// TotalBillableUserTrafficBytes sums each user's last-hop used × billing_rate
// (rate ≤ 0 treated as 1). Same math as the user-detail traffic chip.
func TotalBillableUserTrafficBytes(d *sql.DB) (int64, error) {
	var total int64
	err := d.QueryRow(`
		SELECT COALESCE(SUM(
			CAST(ROUND(u.traffic_used_bytes * CASE WHEN u.billing_rate > 0 THEN u.billing_rate ELSE 1.0 END) AS INTEGER)
		), 0)
		FROM users u
		WHERE u.username <> 'admin'`).Scan(&total)
	return total, err
}

// YesterdayUserTrafficBytes returns one user's raw traffic for the previous
// Asia/Shanghai calendar day. Missing rows are 0.
func YesterdayUserTrafficBytes(d *sql.DB, userID int64) (int64, error) {
	return UserTrafficBytesOnDay(d, userID, dayKey(time.Now().Add(-24*time.Hour)))
}

// DayKeyYesterday is the Asia/Shanghai YYYY-MM-DD for "yesterday".
func DayKeyYesterday() string {
	return dayKey(time.Now().Add(-24 * time.Hour))
}

// NodeRawTraffic returns every node's cumulative raw bytes keyed by id. Nodes
// that never reported a counter batch have no row and are absent from the map.
func NodeRawTraffic(d *sql.DB) (map[int64]int64, error) {
	rows, err := d.Query(`SELECT node_id, raw_bytes FROM node_raw_traffic`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[int64]int64)
	for rows.Next() {
		var nodeID, raw int64
		if err := rows.Scan(&nodeID, &raw); err != nil {
			return nil, err
		}
		m[nodeID] = raw
	}
	return m, rows.Err()
}

// NodeRateMultipliers returns every node's rate_multiplier keyed by id. The
// entry node's value is the whole rule's billing multiplier — middle-layer
// and composite-child hops don't stack their own factors (a composite entry
// carries the baked composite factor on its own column). A stored 0 is a
// deliberate free marker and is returned as-is; billing treats it as free (no
// global usage) and falls back to 1.0 only for a negative or absent value.
func NodeRateMultipliers(d *sql.DB) (map[int64]float64, error) {
	rows, err := d.Query(`SELECT id, rate_multiplier FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]float64{}
	for rows.Next() {
		var id int64
		var mult float64
		if err := rows.Scan(&id, &mult); err != nil {
			return nil, err
		}
		m[id] = mult
	}
	return m, rows.Err()
}

// SegmentFirstHops maps each rule's segment-first hop positions to the
// segment's logical node id. Per-grant byte accounting charges a segment's
// grant exactly once per counter batch — at its first hop — since every hop
// of a segment carries the same bytes.
func SegmentFirstHops(d *sql.DB, ruleIDs []int64) (map[int64]map[int]int64, error) {
	if len(ruleIDs) == 0 {
		return map[int64]map[int]int64{}, nil
	}
	args := make([]any, len(ruleIDs))
	ph := make([]string, len(ruleIDs))
	for i, id := range ruleIDs {
		args[i] = id
		ph[i] = "?"
	}
	rows, err := d.Query(`SELECT rule_id, MIN(position), via_node_id FROM rule_hops
		WHERE rule_id IN (`+strings.Join(ph, ",")+`) GROUP BY rule_id, via_node_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[int64]map[int]int64{}
	for rows.Next() {
		var ruleID, via int64
		var pos int
		if err := rows.Scan(&ruleID, &pos, &via); err != nil {
			return nil, err
		}
		if m[ruleID] == nil {
			m[ruleID] = map[int]int64{}
		}
		m[ruleID][pos] = via
	}
	return m, rows.Err()
}

// CheckAndResetTrafficCycle checks whether the user's traffic reset window has
// elapsed since the last reset. If so, it zeros the global counter, all
// per-node counters, the landing-exit ledger and the displayed per-rule hop
// totals together and records the reset timestamp. rule_hops.last_bytes* are
// kept so the next agent sample still computes a delta. Returns true if a
// reset occurred.
//
// traffic_reset_days == 0 means the user is never auto-reset.
// The window is anchored to the account creation date so the cycle boundary is
// predictable (e.g. "every 30 days from account open date").
func CheckAndResetTrafficCycle(d *sql.DB, u *User) (bool, error) {
	if u.TrafficResetDays <= 0 {
		return false, nil
	}
	nowTs := time.Now().Unix()
	period := int64(u.TrafficResetDays) * 86400
	createdAt := u.CreatedAt
	elapsed := nowTs - createdAt
	if elapsed < 0 {
		return false, nil
	}
	cycleStart := createdAt + (elapsed/period)*period
	if u.LastTrafficResetAt >= cycleStart {
		return false, nil
	}
	tx, err := d.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET traffic_used_bytes = 0, last_traffic_reset_at = ? WHERE id=?`, nowTs, u.ID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE user_nodes SET traffic_used_bytes = 0 WHERE user_id=?`, u.ID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE user_landing_exits SET used_bytes = 0 WHERE user_id=?`, u.ID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE rule_hops SET total_bytes = 0, billed_bytes = 0 WHERE rule_id IN (SELECT id FROM rules WHERE owner_id = ?)`, u.ID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// NodesExceedingQuota returns the IDs of nodes where the user's per-grant
// traffic counter has reached or exceeded the configured quota. Grants with
// quota == 0 (unlimited) are excluded.
func NodesExceedingQuota(d *sql.DB, userID int64) ([]int64, error) {
	return queryInt64s(d,
		`SELECT node_id FROM user_nodes WHERE user_id=? AND traffic_quota_bytes > 0 AND traffic_used_bytes >= traffic_quota_bytes`,
		userID)
}

// RulesAffectedByNode returns the distinct hop-node IDs of all rules owned by
// userID that should be re-pushed when a grant on nodeID changes.
//
// Grants are stored on the node the admin selected in the UI — this can be a
// composite node (via_node_id in rule_hops) or a physical child node (node_id
// in rule_hops or rules.node_id). We match all three so that changing a rate
// limit on any grant level pushes to every physical node carrying the affected
// rules.
func RulesAffectedByNode(d *sql.DB, userID, nodeID int64) ([]int64, error) {
	return queryInt64s(d, `
		SELECT DISTINCT rh.node_id
		FROM rule_hops rh
		JOIN rules r ON r.id = rh.rule_id
		WHERE r.owner_id = ?
		  AND (rh.via_node_id = ? OR rh.node_id = ? OR r.node_id = ?)`,
		userID, nodeID, nodeID, nodeID)
}
