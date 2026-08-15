package db

import "database/sql"

type SubToken struct {
	UserID     int64
	Token      string
	CreatedAt  int64
	LastUsedAt sql.NullInt64
}

func GetSubTokenByUser(d *sql.DB, userID int64) (*SubToken, error) {
	t := &SubToken{}
	err := d.QueryRow(
		`SELECT user_id, token, created_at, last_used_at FROM sub_tokens WHERE user_id=?`,
		userID,
	).Scan(&t.UserID, &t.Token, &t.CreatedAt, &t.LastUsedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func GetUserBySubToken(d *sql.DB, token string) (*User, *SubToken, error) {
	token = trimSubToken(token)
	if token == "" {
		return nil, nil, sql.ErrNoRows
	}
	t := &SubToken{}
	err := d.QueryRow(
		`SELECT user_id, token, created_at, last_used_at FROM sub_tokens WHERE token=?`,
		token,
	).Scan(&t.UserID, &t.Token, &t.CreatedAt, &t.LastUsedAt)
	if err != nil {
		return nil, nil, err
	}
	u, err := GetUserByID(d, t.UserID)
	if err != nil {
		return nil, nil, err
	}
	return u, t, nil
}

// EnsureSubToken returns the existing token or creates one.
func EnsureSubToken(d *sql.DB, userID int64) (string, error) {
	if t, err := GetSubTokenByUser(d, userID); err == nil && t.Token != "" {
		return t.Token, nil
	}
	token := RandToken(24)
	_, err := d.Exec(
		`INSERT INTO sub_tokens(user_id, token, created_at) VALUES (?,?,?)`,
		userID, token, now())
	if err != nil {
		// Race: another request inserted first.
		if t, e2 := GetSubTokenByUser(d, userID); e2 == nil {
			return t.Token, nil
		}
		return "", err
	}
	return token, nil
}

func RotateSubToken(d *sql.DB, userID int64) (string, error) {
	token := RandToken(24)
	res, err := d.Exec(
		`UPDATE sub_tokens SET token=?, created_at=?, last_used_at=NULL WHERE user_id=?`,
		token, now(), userID)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if _, err := d.Exec(
			`INSERT INTO sub_tokens(user_id, token, created_at) VALUES (?,?,?)`,
			userID, token, now()); err != nil {
			return "", err
		}
	}
	return token, nil
}

func TouchSubTokenUsage(d *sql.DB, userID int64) error {
	_, err := d.Exec(`UPDATE sub_tokens SET last_used_at=? WHERE user_id=?`, now(), userID)
	return err
}

func trimSubToken(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
