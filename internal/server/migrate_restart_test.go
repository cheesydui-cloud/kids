package server

import "testing"

func TestPanelRestartModeWithoutUnit(t *testing.T) {
	if panelSystemdUnitPresent() {
		t.Skip("nft-server.service is installed on this machine")
	}
	if got := panelRestartMode(); got != "reexec" {
		t.Fatalf("panelRestartMode() = %q, want reexec", got)
	}
}
