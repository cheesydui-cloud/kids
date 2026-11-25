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

func TestContainerUpgradeScriptAcceptedByOldPanel(t *testing.T) {
	script := containerUpgradeScriptBytes()
	if bytes.Contains(script, []byte("\r")) {
		t.Fatal("container upgrade script contains CR")
	}
	if !bytes.Contains(script, []byte(containerUpgradeMarker)) {
		t.Fatal("missing container upgrade marker")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "nft-upgrade")
	if err := os.WriteFile(path, script, 0o755); err != nil {
		t.Fatal(err)
	}
	if !looksLikePanelInstaller(path) {
		t.Fatal("v0.14.0 would reject this nft-upgrade")
	}
}

func TestInstallContainerUpgradeHelper(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing", "nft-upgrade")
	if err := installContainerUpgradeHelper(missing); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(missing)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, containerUpgradeScriptBytes()) {
		t.Fatal("missing helper was not installed")
	}

	official := filepath.Join(dir, "official.sh")
	body := []byte("#!/bin/bash\n# official\ndo_update(){ echo nft-server; }\n")
	if err := os.WriteFile(official, body, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installContainerUpgradeHelper(official); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(official)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatal("official installer was overwritten")
	}

	marked := filepath.Join(dir, "marked.sh")
	if err := os.WriteFile(marked, []byte("#!/bin/bash\n# kids-container-upgrade\nold\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installContainerUpgradeHelper(marked); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(marked)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, containerUpgradeScriptBytes()) {
		t.Fatal("marked helper was not refreshed")
	}
}

func TestPreferInstallerUpgrade(t *testing.T) {
	official := []byte("#!/bin/bash\ndo_update(){ echo nft-server; }\n")
	helper := append([]byte("# kids-container-upgrade\n"), official...)
	if !preferInstallerUpgrade(true, true, official) {
		t.Fatal("official installer on systemd should be used")
	}
	if preferInstallerUpgrade(true, false, official) {
		t.Fatal("no systemd must use the builtin upgrade")
	}
	if preferInstallerUpgrade(true, true, helper) {
		t.Fatal("container helper must not replace the builtin upgrade")
	}
	if preferInstallerUpgrade(false, true, official) {
		t.Fatal("invalid installer must not be used")
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
