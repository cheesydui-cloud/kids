package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReplaceExecutableWritesNewBytes(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "nft-server")
	if err := os.WriteFile(dest, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(dest, []byte("new-binary-contents")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary-contents" {
		t.Fatalf("dest = %q", got)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".nft-server-new-*"))
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestReplaceErrBusy(t *testing.T) {
	if replaceErrBusy(nil) {
		t.Fatal("nil is not busy")
	}
	if replaceErrBusy(errString("permission denied")) {
		t.Fatal("permission denied is not busy")
	}
	if !replaceErrBusy(errString("remove nft-server: device or resource busy")) {
		t.Fatal("expected device or resource busy")
	}
	if !replaceErrBusy(errString("text file busy")) {
		t.Fatal("expected text file busy")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestReplaceByDirectorySwapKeepsSiblings(t *testing.T) {
	grand := t.TempDir()
	parent := filepath.Join(grand, "sbin")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(parent, "nft-server")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	sib := filepath.Join(parent, "nft-upgrade")
	if err := os.WriteFile(sib, []byte("script"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(grand, "newbin")
	if err := os.WriteFile(src, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceByDirectorySwap(dest, src); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(grand, "sbin", "nft-server"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-binary" {
		t.Fatalf("binary = %q", got)
	}
	sibGot, err := os.ReadFile(filepath.Join(grand, "sbin", "nft-upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sibGot) != "script" {
		t.Fatalf("sibling = %q", sibGot)
	}
}

func TestDownloadPanelReleaseBinaryVerifiesSHA(t *testing.T) {
	payload := make([]byte, 1<<20+8)
	copy(payload, []byte{0x7f, 'E', 'L', 'F'})
	sum := sha256.Sum256(payload)
	good := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			_, _ = w.Write([]byte(good + "  nft-server-linux-amd64\n"))
		case strings.HasSuffix(r.URL.Path, "/nft-server-linux-amd64"):
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prevOS, prevArch, prevBase := panelUpgradeGOOS, panelUpgradeGOARCH, panelReleaseBaseFn
	panelUpgradeGOOS, panelUpgradeGOARCH = "linux", "amd64"
	panelReleaseBaseFn = func(tag string) string { return srv.URL + "/download/" + tag }
	t.Cleanup(func() {
		panelUpgradeGOOS, panelUpgradeGOARCH, panelReleaseBaseFn = prevOS, prevArch, prevBase
	})

	data, name, err := downloadPanelReleaseBinary("v0.15.0")
	if err != nil {
		t.Fatal(err)
	}
	if name != "nft-server-linux-amd64" || len(data) != len(payload) {
		t.Fatalf("name=%s len=%d", name, len(data))
	}
}

func TestDownloadPanelReleaseBinaryRejectsBadSHA(t *testing.T) {
	payload := make([]byte, 1<<20+8)
	copy(payload, []byte{0x7f, 'E', 'L', 'F'})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			_, _ = w.Write([]byte("deadbeef  nft-server-linux-amd64\n"))
		case strings.HasSuffix(r.URL.Path, "/nft-server-linux-amd64"):
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prevOS, prevArch, prevBase := panelUpgradeGOOS, panelUpgradeGOARCH, panelReleaseBaseFn
	panelUpgradeGOOS, panelUpgradeGOARCH = "linux", "amd64"
	panelReleaseBaseFn = func(tag string) string { return srv.URL + "/download/" + tag }
	t.Cleanup(func() {
		panelUpgradeGOOS, panelUpgradeGOARCH, panelReleaseBaseFn = prevOS, prevArch, prevBase
	})

	if _, _, err := downloadPanelReleaseBinary("v0.15.0"); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("err = %v, want sha256 failure", err)
	}
}

func TestDownloadPanelReleaseBinaryRejectsBadTag(t *testing.T) {
	prevOS := panelUpgradeGOOS
	panelUpgradeGOOS = "linux"
	t.Cleanup(func() { panelUpgradeGOOS = prevOS })
	if _, _, err := downloadPanelReleaseBinary("v0.15.0;rm"); err == nil {
		t.Fatal("invalid tag was accepted")
	}
}

func TestRunBuiltinPanelUpgradeReplacesCurrentBinary(t *testing.T) {
	prevDL, prevRep := downloadPanelBinaryFn, replacePanelBinaryFn
	downloadPanelBinaryFn = func(tag string) ([]byte, string, error) {
		if tag != "v0.15.0" {
			t.Fatalf("tag = %s", tag)
		}
		return []byte("new-bytes"), "nft-server-linux-amd64", nil
	}
	var gotPath string
	var gotData []byte
	replacePanelBinaryFn = func(dest string, data []byte) error {
		gotPath = dest
		gotData = append([]byte(nil), data...)
		return nil
	}
	t.Cleanup(func() {
		downloadPanelBinaryFn, replacePanelBinaryFn = prevDL, prevRep
	})

	prevDir := panelUpdateDir
	panelUpdateDir = t.TempDir()
	t.Cleanup(func() { panelUpdateDir = prevDir })

	if err := runBuiltinPanelUpgrade("v0.15.0"); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
		exe = resolved
	}
	if gotPath != exe {
		t.Fatalf("replaced %s, want %s", gotPath, exe)
	}
	if string(gotData) != "new-bytes" {
		t.Fatalf("data = %q", gotData)
	}
}

func TestBuiltinUpgradeRecordsDownloadError(t *testing.T) {
	prevDir := panelUpdateDir
	panelUpdateDir = t.TempDir()
	t.Cleanup(func() { panelUpdateDir = prevDir })

	prevDelay := panelUpgradeDelay
	panelUpgradeDelay = 0
	t.Cleanup(func() { panelUpgradeDelay = prevDelay })

	prevDL := downloadPanelBinaryFn
	downloadPanelBinaryFn = func(string) ([]byte, string, error) {
		return nil, "", errTestDownload
	}
	t.Cleanup(func() { downloadPanelBinaryFn = prevDL })

	if err := startBuiltinPanelUpgrade("v0.15.0", "v0.14.1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := readPanelUpdateStatus()
		if st.State == "error" {
			if !strings.Contains(st.Error, "下载失败") {
				t.Fatalf("error = %q", st.Error)
			}
			if st.Target != "v0.15.0" {
				t.Fatalf("target = %q", st.Target)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for error status")
}

var errTestDownload = stringError("下载失败")

type stringError string

func (e stringError) Error() string { return string(e) }
