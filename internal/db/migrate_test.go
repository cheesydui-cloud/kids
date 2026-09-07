package db

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "live", "panel.db")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := Open(src)
	if err != nil {
		t.Fatal(err)
	}
	uid, err := CreateUser(d, "alice", "hash", "admin")
	if err != nil {
		t.Fatal(err)
	}
	n, err := CreateNode(d, "edge-1", "", "tok-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRule(d, &Rule{NodeID: n.ID, OwnerID: sql.NullInt64{Int64: uid, Valid: true}, Name: "r1", Proto: "tcp", ExitHost: "9.9.9.9", ExitPort: 443}); err != nil {
		t.Fatal(err)
	}
	brand := filepath.Join(dir, "live", "brand")
	docs := filepath.Join(dir, "live", "docs-assets")
	if err := os.MkdirAll(brand, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brand, "logo.png"), []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "aabbccddeeff0011.png"), []byte("doc-img"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	man, err := WriteMigrateArchive(d, brand, docs, "v0.4.7", &buf)
	d.Close()
	if err != nil {
		t.Fatal(err)
	}
	if man.Users < 1 || man.Nodes < 1 || man.Rules != 1 {
		t.Fatalf("manifest counts: %+v", man)
	}
	if man.Files["panel.db"] == "" || man.Files["brand/logo.png"] == "" {
		t.Fatalf("missing file hashes: %+v", man.Files)
	}

	extract := filepath.Join(dir, "extract")
	got, err := ExtractMigrateArchive(bytes.NewReader(buf.Bytes()), extract)
	if err != nil {
		t.Fatal(err)
	}
	if got.Rules != 1 {
		t.Fatalf("extracted rules = %d", got.Rules)
	}
	if _, err := os.Stat(filepath.Join(extract, "brand", "logo.png")); err != nil {
		t.Fatal(err)
	}

	destRoot := filepath.Join(dir, "dest")
	destDB := filepath.Join(destRoot, "panel.db")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destDB, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StageIncoming(destRoot, extract); err != nil {
		t.Fatal(err)
	}
	if err := ApplyStagedMigrate(destDB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destRoot, "migrate-incoming")); !os.IsNotExist(err) {
		t.Fatalf("incoming dir should be gone: %v", err)
	}
	opened, err := Open(destDB)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	u, err := GetUserByUsername(opened, "alice")
	if err != nil || u.Username != "alice" {
		t.Fatalf("imported user: %v %+v", err, u)
	}
	if _, err := os.Stat(filepath.Join(destRoot, "brand", "logo.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destRoot, "docs-assets", "aabbccddeeff0011.png")); err != nil {
		t.Fatal(err)
	}
}

func TestExtractRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	_, err := ExtractMigrateArchive(bytes.NewReader([]byte("not-a-tarball")), dir)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyStagedMigrateNoopWithoutReady(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panel.db")
	if err := ApplyStagedMigrate(dbPath); err != nil {
		t.Fatal(err)
	}
}
