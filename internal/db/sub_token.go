package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SubTokenKeyFile is the on-disk key next to panel.db. Daily VACUUM backups
// copy only the database, so this file is not inside panel-*.db. A full
// directory copy or a migrate archive must carry it, or existing subscription
// URLs cannot be shown again.
const SubTokenKeyFile = "sub_token.key"

const subTokenEncPrefix = "enc1:"

type subKeyMat struct {
	key    []byte
	source string // env, file, ephemeral
}

var subTokenKeys sync.Map // *sql.DB -> *subKeyMat

type SubToken struct {
	UserID     int64
	Token      string
	CreatedAt  int64
	LastUsedAt sql.NullInt64
}

// InitSubTokenKey binds an AES-256 key to this database. NFT_SUB_TOKEN_KEY
// (32 raw bytes, hex, or base64) wins. Otherwise the key file beside a file
// database is created on first use. In-memory databases get an ephemeral key.
func InitSubTokenKey(d *sql.DB, dbPath string) error {
	if d == nil {
		return fmt.Errorf("nil db")
	}
	if raw, ok, err := subTokenKeyFromEnv(); err != nil {
		return err
	} else if ok {
		subTokenKeys.Store(d, &subKeyMat{key: raw, source: "env"})
		return nil
	}
	dir := DataDirFromDBPath(dbPath)
	if dir == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		subTokenKeys.Store(d, &subKeyMat{key: raw, source: "ephemeral"})
		return nil
	}
	path := filepath.Join(dir, SubTokenKeyFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		raw = make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return err
		}
	}
	if len(raw) != 32 {
		return fmt.Errorf("%s must be 32 bytes", path)
	}
	subTokenKeys.Store(d, &subKeyMat{key: append([]byte(nil), raw...), source: "file"})
	return nil
}

// SubTokenKeySource reports where this database's subscription key came from.
func SubTokenKeySource(d *sql.DB) string {
	v, ok := subTokenKeys.Load(d)
	if !ok || v == nil {
		return ""
	}
	return v.(*subKeyMat).source
}

// SubTokenKey returns a copy of the key, for the migrate archive only.
func SubTokenKey(d *sql.DB) []byte {
	v, ok := subTokenKeys.Load(d)
	if !ok || v == nil {
		return nil
	}
	return append([]byte(nil), v.(*subKeyMat).key...)
}

func subTokenKeyFromEnv() ([]byte, bool, error) {
	raw := strings.TrimSpace(os.Getenv("NFT_SUB_TOKEN_KEY"))
	if raw == "" {
		return nil, false, nil
	}
	key, err := parseSubTokenKey(raw)
	if err != nil {
		return nil, false, fmt.Errorf("NFT_SUB_TOKEN_KEY: %w", err)
	}
	return key, true, nil
}

func parseSubTokenKey(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("need 32 bytes as hex, base64, or raw")
}

func subKey(d *sql.DB) ([]byte, error) {
	v, ok := subTokenKeys.Load(d)
	if !ok || v == nil {
		return nil, fmt.Errorf("subscription token key is not initialized")
	}
	return v.(*subKeyMat).key, nil
}

func encryptSubToken(key []byte, plain string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return subTokenEncPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

func decryptSubToken(key []byte, stored string) (string, error) {
	if !strings.HasPrefix(stored, subTokenEncPrefix) {
		return "", fmt.Errorf("not encrypted")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, subTokenEncPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// MigratePlainSubTokens encrypts leftover plaintext subscription tokens and
// fills token_hash. Already-encrypted rows only gain a hash when it is missing.
func MigratePlainSubTokens(d *sql.DB) error {
	key, err := subKey(d)
	if err != nil {
		return err
	}
	rows, err := d.Query(`SELECT user_id, token, token_hash FROM sub_tokens`)
	if err != nil {
		return err
	}
	type row struct {
		userID int64
		token  string
		hash   string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.userID, &r.token, &r.hash); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, r := range pending {
		plain := r.token
		stored := r.token
		if strings.HasPrefix(r.token, subTokenEncPrefix) {
			dec, err := decryptSubToken(key, r.token)
			if err != nil {
				continue
			}
			plain = dec
		} else {
			enc, err := encryptSubToken(key, plain)
			if err != nil {
				return err
			}
			stored = enc
		}
		sum := HashToken(plain)
		if stored == r.token && sum == r.hash {
			continue
		}
		if _, err := d.Exec(`UPDATE sub_tokens SET token=?, token_hash=? WHERE user_id=?`, stored, sum, r.userID); err != nil {
			return err
		}
	}
	return nil
}

func sealSubToken(d *sql.DB, plain string) (stored, hash string, err error) {
	key, err := subKey(d)
	if err != nil {
		return "", "", err
	}
	stored, err = encryptSubToken(key, plain)
	if err != nil {
		return "", "", err
	}
	return stored, HashToken(plain), nil
}

func openSubToken(d *sql.DB, stored string) (string, error) {
	if !strings.HasPrefix(stored, subTokenEncPrefix) {
		return stored, nil
	}
	key, err := subKey(d)
	if err != nil {
		return "", err
	}
	return decryptSubToken(key, stored)
}

func GetSubTokenByUser(d *sql.DB, userID int64) (*SubToken, error) {
	t := &SubToken{}
	var stored string
	err := d.QueryRow(
		`SELECT user_id, token, created_at, last_used_at FROM sub_tokens WHERE user_id=?`,
		userID,
	).Scan(&t.UserID, &stored, &t.CreatedAt, &t.LastUsedAt)
	if err != nil {
		return nil, err
	}
	plain, err := openSubToken(d, stored)
	if err != nil {
		return nil, err
	}
	t.Token = plain
	return t, nil
}

func GetUserBySubToken(d *sql.DB, token string) (*User, *SubToken, error) {
	token = trimSubToken(token)
	if token == "" {
		return nil, nil, sql.ErrNoRows
	}
	t := &SubToken{}
	var stored string
	err := d.QueryRow(
		`SELECT user_id, token, created_at, last_used_at FROM sub_tokens WHERE token_hash=?`,
		HashToken(token),
	).Scan(&t.UserID, &stored, &t.CreatedAt, &t.LastUsedAt)
	if err != nil {
		return nil, nil, err
	}
	plain, err := openSubToken(d, stored)
	if err != nil {
		return nil, nil, err
	}
	t.Token = plain
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
	stored, hash, err := sealSubToken(d, token)
	if err != nil {
		return "", err
	}
	_, err = d.Exec(
		`INSERT INTO sub_tokens(user_id, token, token_hash, created_at) VALUES (?,?,?,?)`,
		userID, stored, hash, now())
	if err != nil {
		if t, e2 := GetSubTokenByUser(d, userID); e2 == nil {
			return t.Token, nil
		}
		return "", err
	}
	return token, nil
}

func RotateSubToken(d *sql.DB, userID int64) (string, error) {
	token := RandToken(24)
	stored, hash, err := sealSubToken(d, token)
	if err != nil {
		return "", err
	}
	res, err := d.Exec(
		`UPDATE sub_tokens SET token=?, token_hash=?, created_at=?, last_used_at=NULL WHERE user_id=?`,
		stored, hash, now(), userID)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		if _, err := d.Exec(
			`INSERT INTO sub_tokens(user_id, token, token_hash, created_at) VALUES (?,?,?,?)`,
			userID, stored, hash, now()); err != nil {
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
