package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompareSemver(t *testing.T) {
	if compareSemver("v0.2.31", "v0.2.32") >= 0 {
		t.Fatal("0.2.31 should be older than 0.2.32")
	}
	if compareSemver("0.2.32", "v0.2.32") != 0 {
		t.Fatal("v prefix must be ignored")
	}
	if compareSemver("v0.3.0", "v0.2.99") <= 0 {
		t.Fatal("0.3.0 should be newer than 0.2.99")
	}
	if !sameVersion("v0.2.31", "0.2.31") {
		t.Fatal("sameVersion should treat v prefix as equal")
	}
}

func TestLooksLikePanelInstaller(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.sh")
	if err := os.WriteFile(good, []byte("#!/usr/bin/env bash\n# kids official installer\n# used by nft-upgrade update\ndo_update() {\n  echo swap nft-server\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !looksLikePanelInstaller(good) {
		t.Fatal("official install.sh should be accepted")
	}
	agent := filepath.Join(dir, "agent.sh")
	if err := os.WriteFile(agent, []byte("#!/usr/bin/env bash\ncurl -fsSL http://panel/v1/install-agent | bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if looksLikePanelInstaller(agent) {
		t.Fatal("node-install wrapper must be rejected")
	}
}

func TestResolvePanelUpdateStatusMarksSuccessOnNewVersion(t *testing.T) {
	prev := panelUpdateDir
	panelUpdateDir = t.TempDir()
	t.Cleanup(func() { panelUpdateDir = prev })

	st := panelUpdateStatus{State: "running", Current: "v0.2.31", Target: "v0.2.32", StartedAt: time.Now().Unix() - 5}
	if err := writePanelUpdateStatus(st); err != nil {
		t.Fatal(err)
	}
	got := resolvePanelUpdateStatus(readPanelUpdateStatus(), "v0.2.32")
	if got.State != "success" {
		t.Fatalf("state=%q, want success after version bump", got.State)
	}
}

func TestResolvePanelUpdateStatusStaleRunning(t *testing.T) {
	prev := panelUpdateDir
	panelUpdateDir = t.TempDir()
	t.Cleanup(func() { panelUpdateDir = prev })

	st := panelUpdateStatus{State: "running", Current: "v0.2.31", Target: "v0.2.32", StartedAt: time.Now().Unix() - 2000}
	if err := writePanelUpdateStatus(st); err != nil {
		t.Fatal(err)
	}
	got := resolvePanelUpdateStatus(readPanelUpdateStatus(), "v0.2.31")
	if got.State != "error" {
		t.Fatalf("state=%q, want error after stale running", got.State)
	}
}

func TestPanelUpdateEndpointsAdminOnlyAndStart(t *testing.T) {
	prevDir := panelUpdateDir
	panelUpdateDir = t.TempDir()
	t.Cleanup(func() { panelUpdateDir = prevDir })

	prevFetch := fetchLatestReleaseFn
	fetchLatestReleaseFn = func() (*githubReleaseInfo, error) {
		return &githubReleaseInfo{Tag: "v9.9.9", Notes: "test notes", HTMLURL: "https://example.com/r"}, nil
	}
	t.Cleanup(func() { fetchLatestReleaseFn = prevFetch })

	started := false
	prevStart := startPanelUpgradeFn
	startPanelUpgradeFn = func(target, current string) error {
		started = true
		if target != "v9.9.9" {
			t.Fatalf("target=%q", target)
		}
		return nil
	}
	t.Cleanup(func() { startPanelUpgradeFn = prevStart })

	d := openDB(t)
	s := newServer(t, d)
	_, userCookie := loginAsUser(t, d, 10)
	admin := loginAsAdmin(t, d)

	req := newTestRequest("GET", "/api/settings/update", nil)
	req.AddCookie(userCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("user GET update: %d, want 403", rec.Code)
	}

	req = newTestRequest("GET", "/api/settings/update", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin GET update: %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["latest"] != "v9.9.9" {
		t.Fatalf("latest=%v", got["latest"])
	}
	if got["up_to_date"] == true {
		t.Fatal("dev/current should not be up_to_date vs v9.9.9 unless versions match")
	}

	req = newTestRequest("POST", "/api/settings/update", bytes.NewReader(nil))
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST update: %d body=%s", rec.Code, rec.Body.String())
	}
	if !started {
		t.Fatal("upgrade was not started")
	}
	st := readPanelUpdateStatus()
	if st.State != "running" || st.Target != "v9.9.9" {
		t.Fatalf("status=%+v", st)
	}

	req = newTestRequest("POST", "/api/settings/update", bytes.NewReader(nil))
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second POST: %d, want 409", rec.Code)
	}
}
