package db

import (
	"database/sql"
	"strings"
	"unicode/utf8"
)

const (
	RequestRenew = "renew"
	RequestQuota = "quota"
)

type UserRequest struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Kind      string `json:"kind"`
	Note      string `json:"note"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

func ValidRequestKind(kind string) bool {
	return kind == RequestRenew || kind == RequestQuota
}

// CreateUserRequest opens one request of this kind. A still-open row is
// returned as-is so a user cannot stack duplicates.
func CreateUserRequest(d *sql.DB, userID int64, kind, note string) (*UserRequest, bool, error) {
	kind = strings.TrimSpace(kind)
	if !ValidRequestKind(kind) {
		return nil, false, sql.ErrNoRows
	}
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 200 {
		note = string([]rune(note)[:200])
	}
	existing, err := openUserRequest(d, userID, kind)
	if err != nil {
		return nil, false, err
	}
	if existing != nil {
		return existing, true, nil
	}
	res, err := d.Exec(
		`INSERT INTO user_requests(user_id, kind, note, status, created_at) VALUES (?,?,?,'open',?)`,
		userID, kind, note, now())
	if err != nil {
		return nil, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, err
	}
	row, err := GetUserRequest(d, id)
	return row, false, err
}

func openUserRequest(d *sql.DB, userID int64, kind string) (*UserRequest, error) {
	row := &UserRequest{}
	err := d.QueryRow(
		`SELECT id, user_id, kind, note, status, created_at FROM user_requests
		 WHERE user_id=? AND kind=? AND status='open' ORDER BY id DESC LIMIT 1`,
		userID, kind).Scan(&row.ID, &row.UserID, &row.Kind, &row.Note, &row.Status, &row.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

func GetUserRequest(d *sql.DB, id int64) (*UserRequest, error) {
	row := &UserRequest{}
	err := d.QueryRow(
		`SELECT id, user_id, kind, note, status, created_at FROM user_requests WHERE id=?`, id,
	).Scan(&row.ID, &row.UserID, &row.Kind, &row.Note, &row.Status, &row.CreatedAt)
	if err != nil {
		return nil, err
	}
	return row, nil
}

// ListUserRequests returns open requests first, then the newest closed ones.
func ListUserRequests(d *sql.DB, userID int64) ([]UserRequest, error) {
	rows, err := d.Query(
		`SELECT id, user_id, kind, note, status, created_at FROM user_requests
		 WHERE user_id=? ORDER BY CASE status WHEN 'open' THEN 0 ELSE 1 END, id DESC LIMIT 20`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserRequest{}
	for rows.Next() {
		var row UserRequest
		if err := rows.Scan(&row.ID, &row.UserID, &row.Kind, &row.Note, &row.Status, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func CloseUserRequest(d *sql.DB, userID, id int64) error {
	res, err := d.Exec(`UPDATE user_requests SET status='done' WHERE id=? AND user_id=? AND status='open'`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
