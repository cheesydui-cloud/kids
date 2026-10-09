package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSubTokenEncryptRoundTrip(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	token, err := EnsureSubToken(d, uid)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := d.QueryRow(`SELECT token FROM sub_tokens WHERE user_id=?`, uid).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "enc1:") {
		t.Fatalf("stored token = %q, want enc1: prefix", stored)
	}
	if strings.Contains(stored, token) {
		t.Fatal("ciphertext contains the plaintext token")
	}
	u, got, err := GetUserBySubToken(d, token)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != uid || got.Token != token {
		t.Fatalf("lookup got user %d token %q", u.ID, got.Token)
	}
	if _, _, err := GetUserBySubToken(d, token+"x"); err != sql.ErrNoRows {
		t.Fatalf("wrong token err = %v, want sql.ErrNoRows", err)
	}
}

func TestMigratePlainSubTokens(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	plain := "legacy-sub-token"
	if _, err := d.Exec(
		`INSERT INTO sub_tokens(user_id, token, token_hash, created_at) VALUES (?,?, '', ?)`,
		uid, plain, time.Now().Unix(),
	); err != nil {
		t.Fatal(err)
	}
	if err := MigratePlainSubTokens(d); err != nil {
		t.Fatal(err)
	}
	var stored, hash string
	if err := d.QueryRow(`SELECT token, token_hash FROM sub_tokens WHERE user_id=?`, uid).Scan(&stored, &hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, "enc1:") {
		t.Fatalf("after migrate token = %q", stored)
	}
	if hash != HashToken(plain) {
		t.Fatalf("hash = %s", hash)
	}
	u, got, err := GetUserBySubToken(d, plain)
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != uid || got.Token != plain {
		t.Fatalf("legacy lookup got user %d token %q", u.ID, got.Token)
	}
}

func TestUserTrafficLastDaysFillsZeros(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	today := dayKey(time.Now())
	if _, err := d.Exec(
		`INSERT INTO daily_user_traffic(day, user_id, raw_bytes) VALUES(?,?,?)`,
		today, uid, 42,
	); err != nil {
		t.Fatal(err)
	}
	days, err := UserTrafficLastDays(d, uid, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 7 {
		t.Fatalf("len = %d", len(days))
	}
	if days[0].Day >= days[6].Day {
		t.Fatalf("order %s .. %s", days[0].Day, days[6].Day)
	}
	if days[6].Day != today || days[6].RawBytes != 42 {
		t.Fatalf("today = %+v", days[6])
	}
	for i := 0; i < 6; i++ {
		if days[i].RawBytes != 0 {
			t.Fatalf("day %s raw = %d", days[i].Day, days[i].RawBytes)
		}
	}
}

func TestCreateUserRequestDedupe(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	note := strings.Repeat("字", 201)
	row, already, err := CreateUserRequest(d, uid, RequestRenew, note)
	if err != nil {
		t.Fatal(err)
	}
	if already || row == nil {
		t.Fatalf("first already=%v row=%v", already, row)
	}
	if utf8.RuneCountInString(row.Note) != 200 {
		t.Fatalf("note runes = %d", utf8.RuneCountInString(row.Note))
	}
	again, already, err := CreateUserRequest(d, uid, RequestRenew, "second")
	if err != nil {
		t.Fatal(err)
	}
	if !already || again.ID != row.ID || again.Note != row.Note {
		t.Fatalf("dedupe got id=%d already=%v note=%q", again.ID, already, again.Note)
	}
	if err := CloseUserRequest(d, uid, row.ID); err != nil {
		t.Fatal(err)
	}
	if err := CloseUserRequest(d, uid, row.ID); err != sql.ErrNoRows {
		t.Fatalf("second close = %v", err)
	}
}

func TestReadBackupStatusCountsSnapshots(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "panel-20261008-120000.db"), []byte("snap"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := ReadBackupStatus(dir)
	if st.Count != 1 || st.LatestName != "panel-20261008-120000.db" || st.LatestUnix == 0 {
		t.Fatalf("status = %+v", st)
	}
}
