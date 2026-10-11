package db

import "database/sql"

// NodeImpact is how many rules and distinct rule owners touch a node.
// A middle hop counts even when it is not the rule's entry.
type NodeImpact struct {
	Rules int
	Users int
}

// NodeImpactByNode counts rules and owners per node. Entry rows and hop rows
// are unioned so a node is counted once per rule.
func NodeImpactByNode(d *sql.DB) (map[int64]NodeImpact, error) {
	rows, err := d.Query(`
		SELECT node_id, COUNT(DISTINCT rule_id), COUNT(DISTINCT owner_id)
		FROM (
			SELECT node_id, id AS rule_id, owner_id FROM rules
			UNION ALL
			SELECT h.node_id, h.rule_id, r.owner_id
			FROM rule_hops h
			JOIN rules r ON r.id = h.rule_id
		)
		GROUP BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]NodeImpact{}
	for rows.Next() {
		var id int64
		var rules, users int
		if err := rows.Scan(&id, &rules, &users); err != nil {
			return nil, err
		}
		out[id] = NodeImpact{Rules: rules, Users: users}
	}
	return out, rows.Err()
}

// NextHopIDs returns distinct nodes that a hop on nodeID dials in a deployed rule.
func NextHopIDs(d DBTX, nodeID int64) ([]int64, error) {
	return hopPeerIDs(d, `
		SELECT DISTINCT h2.node_id
		FROM rule_hops h1
		JOIN rule_hops h2 ON h1.rule_id = h2.rule_id AND h2.position = h1.position + 1
		WHERE h1.node_id=?
		ORDER BY h2.node_id`, nodeID)
}

// PrevHopIDs returns distinct nodes that dial nodeID in a deployed rule.
func PrevHopIDs(d DBTX, nodeID int64) ([]int64, error) {
	return hopPeerIDs(d, `
		SELECT DISTINCT h1.node_id
		FROM rule_hops h1
		JOIN rule_hops h2 ON h1.rule_id = h2.rule_id AND h2.position = h1.position + 1
		WHERE h2.node_id=?
		ORDER BY h1.node_id`, nodeID)
}

func hopPeerIDs(d DBTX, query string, nodeID int64) ([]int64, error) {
	rows, err := d.Query(query, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// NodeAlertState is the last alert decision stored for one node.
type NodeAlertState struct {
	NodeID         int64
	OfflineSince   int64
	OfflineAlerted bool
	LastError      string
}

// LoadNodeAlertStates returns every persisted alert row.
func LoadNodeAlertStates(d *sql.DB) (map[int64]NodeAlertState, error) {
	rows, err := d.Query(`SELECT node_id, offline_since, offline_alerted, last_error FROM node_alert_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]NodeAlertState{}
	for rows.Next() {
		var st NodeAlertState
		var alerted int
		if err := rows.Scan(&st.NodeID, &st.OfflineSince, &alerted, &st.LastError); err != nil {
			return nil, err
		}
		st.OfflineAlerted = alerted != 0
		out[st.NodeID] = st
	}
	return out, rows.Err()
}

// SaveNodeAlertState upserts one node's alert clock.
func SaveNodeAlertState(d *sql.DB, st NodeAlertState) error {
	alerted := 0
	if st.OfflineAlerted {
		alerted = 1
	}
	_, err := d.Exec(`
		INSERT INTO node_alert_state (node_id, offline_since, offline_alerted, last_error)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			offline_since=excluded.offline_since,
			offline_alerted=excluded.offline_alerted,
			last_error=excluded.last_error`,
		st.NodeID, st.OfflineSince, alerted, st.LastError)
	return err
}

// DeleteNodeAlertState drops a row that no longer applies (disabled, composite).
func DeleteNodeAlertState(d *sql.DB, nodeID int64) error {
	_, err := d.Exec(`DELETE FROM node_alert_state WHERE node_id=?`, nodeID)
	return err
}
