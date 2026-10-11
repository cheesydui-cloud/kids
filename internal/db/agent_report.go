package db

import "database/sql"

// SetNodeDisconnect records why the agent last dropped the control channel.
// Callers skip this when the hello omits the field so an old agent does not
// wipe a reason written by a newer build.
func SetNodeDisconnect(d DBTX, id int64, reason string, at int64) error {
	if at < 0 {
		at = 0
	}
	_, err := d.Exec(`UPDATE nodes SET last_disconnect_reason=?, last_disconnect_at=? WHERE id=?`, reason, at, id)
	return err
}

// SetNodeEgressNote stores or clears the "relay address did not follow the
// connect IP" note. Empty clears it.
func SetNodeEgressNote(d DBTX, id int64, note string) error {
	_, err := d.Exec(`UPDATE nodes SET egress_note=? WHERE id=?`, note, id)
	return err
}

// SetNodeResourceWarning stores or clears the agent's resource watermark.
// Callers must only invoke this when the ping JSON actually contained the
// key — an omitted key (old agent, apply keepalive) must not clear it.
func SetNodeResourceWarning(d DBTX, id int64, warning string) error {
	_, err := d.Exec(`UPDATE nodes SET resource_warning=? WHERE id=?`, warning, id)
	return err
}

// MarkNodeUpgradeRolledBack records that the new agent binary failed to stay
// up and restored the previous one. The target version is left in place so
// the detail page can still say which push was rolled back.
func MarkNodeUpgradeRolledBack(d DBTX, nodeID int64) error {
	_, err := d.Exec(
		`UPDATE nodes SET last_upgrade_at=?, last_upgrade_status=?, last_upgrade_error=? WHERE id=?`,
		now(), "rolled_back", "新版本未能启动，已退回上一版", nodeID)
	return err
}

// AgentReport is the four operator fields added in migration 0070.
// Kept as a named result so tests can assert the scanners together.
type AgentReport struct {
	LastDisconnectReason string
	LastDisconnectAt     int64
	EgressNote           string
	ResourceWarning      string
}

// ReadAgentReport loads the 0070 columns for one node. Tests use it to
// confirm a setter landed without depending on the full node scan.
func ReadAgentReport(d *sql.DB, id int64) (AgentReport, error) {
	var r AgentReport
	err := d.QueryRow(
		`SELECT last_disconnect_reason, last_disconnect_at, egress_note, resource_warning FROM nodes WHERE id=?`,
		id).Scan(&r.LastDisconnectReason, &r.LastDisconnectAt, &r.EgressNote, &r.ResourceWarning)
	return r, err
}
