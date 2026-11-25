package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallScriptDoesNotCopyOntoBusyEntrypoint(t *testing.T) {
	script := string(readRepoFile(t, "scripts", "install-container-panel.sh"))
	if strings.Contains(script, ":/usr/local/sbin/nft-server") {
		t.Fatal("installer must not docker cp onto the running entrypoint")
	}
	for _, needle := range []string{
		"NFT_INSTALL_LOCAL",
		"NFT_FORCE_RECREATE",
		"NFT_SERVER_BIN",
		"NFT_UPGRADE_FILE",
		"NFT_STARTER_FILE",
		"NFT_EXPECT_FILE",
		"NFT_EXPECT_TEXT",
		"KIDS_SWITCH=upper",
		"KIDS_SWITCH=next",
		"kids-docker-init",
		"io.containerd.snapshotter",
		"set -euo pipefail",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("installer missing %s", needle)
		}
	}
}

func TestContainerUpgradeOnOverlay(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker overlay test in short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("overlay proof runs on linux")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		if os.Getenv("CI") == "true" {
			t.Fatal("CI requires docker to prove the container upgrade")
		}
		t.Skip("docker not available")
	}
	script := filepath.Join(repoRoot(t), "scripts", "test-container-upgrade.sh")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("container upgrade test failed: %v\n%s", err, out)
	}
}

func readRepoFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	path := filepath.Join(append([]string{repoRoot(t)}, parts...)...)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
