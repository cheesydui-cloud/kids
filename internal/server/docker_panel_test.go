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

func TestInstallDockerPanelScript(t *testing.T) {
	script := string(readRepoFile(t, "scripts", "install-docker-panel.sh"))
	initScript := string(readRepoFile(t, "docker", "panel", "docker-init.sh"))
	retired := string(readRepoFile(t, "docker", "panel", "fetch-panel.sh"))
	for _, needle := range []string{
		"127.0.0.1:7788:7788",
		"--cap-drop ALL",
		"no-new-privileges:true",
		"kids-docker-init",
		"bootstrap-admin-password",
		"--reuid=kids",
		"set -euo pipefail",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("install-docker-panel.sh missing %s", needle)
		}
	}
	if strings.Contains(script, "--env-file") {
		t.Fatal("installer must not pass the bootstrap password through docker env")
	}
	if strings.Contains(script, "--inh-caps") {
		t.Fatal("setpriv must not require CAP_SETPCAP")
	}
	for _, needle := range []string{"# kids-docker-init", "--reuid=kids", "bootstrap-admin-password", "chmod 600"} {
		if !strings.Contains(initScript, needle) {
			t.Fatalf("docker-init.sh missing %s", needle)
		}
	}
	if !strings.Contains(retired, "已停用") || !strings.Contains(retired, "exit 1") {
		t.Fatal("fetch-panel.sh must refuse to install a binary as the entrypoint")
	}
	if strings.Contains(retired, "/usr/local/bin/nft-server") && !strings.Contains(retired, "Retired") {
		t.Fatal("fetch-panel.sh still installs the old entrypoint")
	}
}

func TestDockerPanelInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping docker panel test in short mode")
	}
	if runtime.GOOS != "linux" {
		t.Skip("docker panel proof runs on linux")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		if os.Getenv("CI") == "true" {
			t.Fatal("CI requires docker to prove the panel image")
		}
		t.Skip("docker not available")
	}
	script := filepath.Join(repoRoot(t), "scripts", "test-docker-panel.sh")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker panel test failed: %v\n%s", err, out)
	}
}
