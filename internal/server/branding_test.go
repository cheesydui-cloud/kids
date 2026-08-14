package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"nft/internal/db"
)

func TestBrandingLogoUploadAndClear(t *testing.T) {
	d := openDB(t)
	tmp := t.TempDir()
	docsDir := filepath.Join(tmp, "docs-assets")
	s, err := NewWithDocsDir(d, docsDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop() })

	admin := loginAsAdmin(t, d)

	req := newTestRequest("GET", "/api/branding", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public branding: %d %s", rec.Code, rec.Body.String())
	}
	var brand struct {
		PanelName string `json:"panel_name"`
		LogoURL   string `json:"logo_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &brand); err != nil {
		t.Fatal(err)
	}
	if brand.LogoURL != "" {
		t.Fatalf("expected empty logo before upload, got %q", brand.LogoURL)
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "mark.png")
	if err != nil {
		t.Fatal(err)
	}
	png := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0, 0, 0, 0xd, 'I', 'H', 'D', 'R',
		0, 0, 0, 1, 0, 0, 0, 1, 8, 2, 0, 0, 0,
	}
	png = append(png, bytes.Repeat([]byte{0}, 64)...)
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	req = newTestRequest("POST", "/api/settings/logo", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.AddCookie(admin)
	up := httptest.NewRecorder()
	s.Router().ServeHTTP(up, req)
	if up.Code != http.StatusOK {
		t.Fatalf("upload logo: %d %s", up.Code, up.Body.String())
	}
	var upResp struct {
		LogoURL string `json:"logo_url"`
	}
	if err := json.Unmarshal(up.Body.Bytes(), &upResp); err != nil || upResp.LogoURL != "/api/branding/logo" {
		t.Fatalf("upload parse: %v %s", err, up.Body.String())
	}
	stored, _ := db.GetSetting(d, panelLogoSetting)
	if stored != "logo.png" {
		t.Fatalf("stored logo name = %q", stored)
	}
	if _, err := os.Stat(filepath.Join(tmp, "brand", "logo.png")); err != nil {
		t.Fatalf("logo missing on disk: %v", err)
	}

	req = newTestRequest("GET", "/api/branding/logo", nil)
	get := httptest.NewRecorder()
	s.Router().ServeHTTP(get, req)
	if get.Code != http.StatusOK {
		t.Fatalf("serve logo: %d %s", get.Code, get.Body.String())
	}
	data, _ := io.ReadAll(get.Body)
	if len(data) < 8 || data[0] != 0x89 {
		t.Fatalf("bad logo body len=%d", len(data))
	}

	rec = adminJSON(t, s, admin, "GET", "/api/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings: %d %s", rec.Code, rec.Body.String())
	}
	var settings struct {
		LogoURL string `json:"logo_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil || settings.LogoURL != "/api/branding/logo" {
		t.Fatalf("settings logo: %v %s", err, rec.Body.String())
	}

	req = newTestRequest("DELETE", "/api/settings/logo", nil)
	req.AddCookie(admin)
	del := httptest.NewRecorder()
	s.Router().ServeHTTP(del, req)
	if del.Code != http.StatusOK {
		t.Fatalf("clear logo: %d %s", del.Code, del.Body.String())
	}
	if name, _ := db.GetSetting(d, panelLogoSetting); name != "" {
		t.Fatalf("logo setting still set: %q", name)
	}
	req = newTestRequest("GET", "/api/branding/logo", nil)
	missing := httptest.NewRecorder()
	s.Router().ServeHTTP(missing, req)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("cleared logo should 404, got %d", missing.Code)
	}
}

func TestCreateNodeDefaultPortRange(t *testing.T) {
	d := openDB(t)
	n, err := db.CreateNode(d, "edge-default", "", "tok-default")
	if err != nil {
		t.Fatal(err)
	}
	if n.PortRange != db.DefaultPortRange {
		t.Fatalf("new node port_range = %q, want %q", n.PortRange, db.DefaultPortRange)
	}
}
