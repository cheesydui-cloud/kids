package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitialPasswordNoticeHidesPasswordWithoutTTY(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panel.db")
	const pw = "generated-secret"
	notice, err := initialPasswordNotice(dbPath, pw, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(notice, pw) {
		t.Fatalf("non-TTY notice contains the password: %s", notice)
	}
	if !strings.Contains(notice, "initial-admin-password") {
		t.Fatalf("notice = %q", notice)
	}
	path := filepath.Join(dir, "initial-admin-password")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != pw+"\n" {
		t.Fatalf("file = %q", b)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %o, want 600", st.Mode().Perm())
		}
	}

	ttyNotice, err := initialPasswordNotice(dbPath, pw, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ttyNotice, pw) {
		t.Fatal("TTY notice should show the generated password once")
	}
}

func TestConsumeBootstrapPasswordFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panel.db")
	path := filepath.Join(dir, "bootstrap-admin-password")
	if err := os.WriteFile(path, []byte("  from-file \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := consumeBootstrapPasswordFile(dbPath); got != "from-file" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("password file still present: %v", err)
	}
	if got := consumeBootstrapPasswordFile(dbPath); got != "" {
		t.Fatalf("second read = %q", got)
	}
}
