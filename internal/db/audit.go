package db

import (
	"database/sql"
	"strings"
)

// AuditLog is one operator-visible audit record, joined with the actor's name.
type AuditLog struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Action   string `json:"action"`
	Target   string `json:"target"`
	Payload  string `json:"payload"`
	At       int64  `json:"at"`
}

// AuditFilter narrows an audit query. Zero values disable that filter.
type AuditFilter struct {
	Query  string
	UserID int64
	From   int64
	To     int64
	Limit  int
	Offset int
}

// ListAuditLogs returns one page of audit rows (newest first) plus the total
// number of matching rows, so the UI can render a pager.
func ListAuditLogs(d *sql.DB, f AuditFilter) ([]AuditLog, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + q + "%"
		where = append(where, "(a.action LIKE ? OR a.target LIKE ? OR a.payload LIKE ? OR COALESCE(u.username,'') LIKE ?)")
		args = append(args, like, like, like, like)
	}
	if f.UserID > 0 {
		where = append(where, "a.user_id = ?")
		args = append(args, f.UserID)
	}
	if f.From > 0 {
		where = append(where, "a.at >= ?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		where = append(where, "a.at <= ?")
		args = append(args, f.To)
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	joined := ` FROM audit_logs a LEFT JOIN users u ON u.id = a.user_id` + clause

	var total int
	if err := d.QueryRow(`SELECT COUNT(*)`+joined, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := d.Query(`SELECT a.id, COALESCE(a.user_id, 0), COALESCE(u.username, ''), a.action,
			COALESCE(a.target, ''), COALESCE(a.payload, ''), a.at`+joined+
		` ORDER BY a.at DESC, a.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Action, &l.Target, &l.Payload, &l.At); err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	return out, total, rows.Err()
}
