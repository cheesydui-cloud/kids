// Package agentbin optionally embeds linux nft-agent binaries into nft-server
// so a freshly installed panel can hand agents to nodes without GitHub.
// Release builds overwrite linux-amd64 / linux-arm64 with real binaries
// immediately before compiling nft-server. Dev / CI keep tiny placeholders
// so `go test` still compiles; Linux() then reports missing.
package agentbin

import (
	_ "embed"
)

//go:embed linux-amd64
var linuxAmd64 []byte

//go:embed linux-arm64
var linuxArm64 []byte

// minEmbedded is well below a real static Go binary (~8MB) and well above
// the placeholder files checked into git.
const minEmbedded = 1 << 20

// Linux returns the embedded nft-agent for GOARCH when the release build
// actually packed one in.
func Linux(arch string) ([]byte, bool) {
	var b []byte
	switch arch {
	case "amd64":
		b = linuxAmd64
	case "arm64":
		b = linuxArm64
	default:
		return nil, false
	}
	if len(b) < minEmbedded {
		return nil, false
	}
	return b, true
}
