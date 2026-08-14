package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nft/internal/db"
	"nft/internal/installscript"
)

func TestUpgradeForLabelSyncWhenShaMatches(t *testing.T) {
	art := &agentArtifact{Version: "v1.0.0", SHA: "abc123", Data: []byte("agent-bytes")}

	// Node already on the target binary: label-only sync, no download URL.
	match := upgradeFor(&db.Node{AgentSHA: "abc123"}, art, "https://panel", "amd64")
	if len(match.Data) != 0 || match.DownloadAt != "" {
		t.Fatalf("matched sha must be label-only, got data=%d downloadAt=%q", len(match.Data), match.DownloadAt)
	}
	if match.Version != "v1.0.0" || match.SHA256 != "abc123" {
		t.Fatalf("label-only must still carry version+sha: %+v", match)
	}

	// Different binary: metadata + HTTP download URL, never inline the agent
	// on the control WS (avoids multi-MB write timeouts on slow links).
	full := upgradeFor(&db.Node{AgentSHA: "stale"}, art, "https://panel/", "amd64")
	if len(full.Data) != 0 {
		t.Fatalf("mismatched sha must not inline bytes, got %d", len(full.Data))
	}
	if full.DownloadAt != "https://panel/v1/binary?arch=amd64" || full.Size != int64(len(art.Data)) {
		t.Fatalf("mismatched sha must point at panel binary: %+v", full)
	}

	// Legacy agent with no reported sha: still must offer a download (can't assume match).
	legacy := upgradeFor(&db.Node{AgentSHA: ""}, art, "https://panel", "amd64")
	if legacy.DownloadAt == "" || legacy.Size != int64(len(art.Data)) {
		t.Fatalf("empty agent sha must offer download, got %+v", legacy)
	}
	if len(legacy.Data) != 0 {
		t.Fatalf("empty agent sha must not inline bytes")
	}

	// Bare host:port (common panel_url setting) must gain a scheme or agent
	// net/http rejects "ip:port/v1/binary" as an invalid URL path segment.
	bare := upgradeFor(&db.Node{AgentSHA: "stale"}, art, "107.174.202.136:7788", "amd64")
	if bare.DownloadAt != "http://107.174.202.136:7788/v1/binary?arch=amd64" {
		t.Fatalf("bare host:port must normalize to http URL, got %q", bare.DownloadAt)
	}
	// Explicit https is preserved; trailing slash stripped.
	https := upgradeFor(&db.Node{AgentSHA: "stale"}, art, "https://panel.example/", "arm64")
	if https.DownloadAt != "https://panel.example/v1/binary?arch=arm64" {
		t.Fatalf("https panel_url: got %q", https.DownloadAt)
	}
}

func TestServeInstallAgentBakesPanelURL(t *testing.T) {
	if !strings.Contains(installscript.AgentInstall, "__KIDS_PANEL_URL__") {
		t.Fatal("installer template missing panel placeholder")
	}
	s := &Server{}
	req := httptest.NewRequest("GET", "/v1/install-agent", nil)
	req.Host = "31.40.214.186:7788"
	rec := httptest.NewRecorder()
	s.serveInstallAgent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "http://31.40.214.186:7788") {
		t.Fatalf("baked panel url missing: %s", body[:min(200, len(body))])
	}
	if strings.Contains(body, "__KIDS_PANEL_URL__") {
		t.Fatal("placeholder left in served script")
	}
}

func TestNormalizeAgentArch(t *testing.T) {
	cases := map[string]string{
		"":        "amd64",
		"x86_64":  "amd64",
		"AMD64":   "amd64",
		"aarch64": "arm64",
		"arm64":   "arm64",
		"ppc64":   "amd64",
	}
	for in, want := range cases {
		if got := normalizeAgentArch(in); got != want {
			t.Errorf("normalizeAgentArch(%q)=%q want %q", in, got, want)
		}
	}
}

func TestNormalizePanelBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"https://panel.example", "https://panel.example"},
		{"https://panel.example/", "https://panel.example"},
		{"http://1.2.3.4:7788/", "http://1.2.3.4:7788"},
		{"1.2.3.4:7788", "http://1.2.3.4:7788"},
		{"panel.example", "http://panel.example"},
	}
	for _, tc := range cases {
		if got := normalizePanelBaseURL(tc.in); got != tc.want {
			t.Errorf("normalizePanelBaseURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
