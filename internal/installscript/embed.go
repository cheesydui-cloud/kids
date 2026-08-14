package installscript

import _ "embed"

// AgentInstall is the node-only installer served at /v1/install-agent.
//
//go:embed install-agent.sh
var AgentInstall string
