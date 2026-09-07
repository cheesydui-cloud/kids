package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nft/internal/db"
)

func TestMigrateExportImportRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	docsDir := filepath.Join(tmp, "docs-assets")
	d, err := db.Open(filepath.Join(tmp, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, err := NewWithDocsDir(d, docsDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	admin := loginAsAdmin(t, d)

	n, err := db.CreateNode(d, "edge-m", "", "tok-m")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := db.GetUserByUsername(d, "admin-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateRule(d, &db.Rule{NodeID: n.ID, OwnerID: sql.NullInt64{Int64: uid.ID, Valid: true}, Name: "mr", Proto: "tcp", ExitHost: "1.1.1.1", ExitPort: 443}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "brand"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "brand", "logo.png"), []byte("logo"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := newTestRequest("GET", "/api/settings/migrate", nil)
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "gzip") {
		t.Fatalf("content-type = %q", ct)
	}
	archive := rec.Body.Bytes()
	if len(archive) < 32 {
		t.Fatalf("archive too small: %d", len(archive))
	}

	restarted := false
	old := restartPanelFn
	restartPanelFn = func() error { restarted = true; return nil }
	t.Cleanup(func() { restartPanelFn = old })

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("confirm", "覆盖本机数据")
	part, err := w.CreateFormFile("file", "kids-migrate.tgz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(archive); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	req = newTestRequest("POST", "/api/settings/migrate", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(admin)
	up := httptest.NewRecorder()
	s.Router().ServeHTTP(up, req)
	if up.Code != http.StatusOK {
		t.Fatalf("import: %d %s", up.Code, up.Body.String())
	}
	if !restarted {
		t.Fatal("expected restart")
	}
	if _, err := os.Stat(filepath.Join(tmp, "migrate-incoming", "ready")); err != nil {
		t.Fatalf("staged marker: %v", err)
	}
	var resp struct {
		OK    bool `json:"ok"`
		Users int  `json:"users"`
		Rules int  `json:"rules"`
	}
	if err := json.Unmarshal(up.Body.Bytes(), &resp); err != nil || !resp.OK || resp.Rules != 1 {
		t.Fatalf("import body: %v %s", err, up.Body.String())
	}
}

func TestMigrateImportRejectsBadConfirmAndUser(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s, err := NewWithDocsDir(d, filepath.Join(tmp, "docs-assets"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })
	admin := loginAsAdmin(t, d)
	_, userCookie := loginAsUser(t, d, 10)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("confirm", "yes")
	part, _ := w.CreateFormFile("file", "x.tgz")
	_, _ = io.WriteString(part, "xxx")
	_ = w.Close()
	req := newTestRequest("POST", "/api/settings/migrate", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad confirm: %d %s", rec.Code, rec.Body.String())
	}

	req = newTestRequest("POST", "/api/settings/migrate", bytes.NewReader(nil))
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user import: %d", rec.Code)
	}
}
