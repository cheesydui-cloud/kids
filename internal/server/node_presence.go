package server

import (
	"fmt"
	"time"

	"nft/internal/db"
)

// presenceView is the live slice of a node the line list merges from the
// speed channel. Strings are always sent, including empty, so the client can
// clear a recovered error or warning.
type presenceView struct {
	ID                   int64    `json:"id"`
	Online               int      `json:"online"`
	LastSeen             int64    `json:"last_seen"`
	LastApplyAt          int64    `json:"last_apply_at"`
	LastError            string   `json:"last_error"`
	LastWarning          string   `json:"last_warning"`
	LastDisconnectReason string   `json:"last_disconnect_reason"`
	EgressNote           string   `json:"egress_note"`
	ResourceWarning      string   `json:"resource_warning"`
	AgentVersion         string   `json:"agent_version"`
	AgentSHA             string   `json:"agent_sha"`
	UpgradeAttention     string   `json:"upgrade_attention"`
	Disabled             bool     `json:"disabled"`
	DownHops             []string `json:"down_hops"`
}

func (s *Server) nodePresence() []presenceView {
	s.presenceMu.Lock()
	defer s.presenceMu.Unlock()
	if s.presenceSnap != nil && time.Since(s.presenceAt) < 2*time.Second {
		return s.presenceSnap
	}
	nodes, err := db.ListNodes(s.DB)
	if err != nil {
		return s.presenceSnap
	}
	s.reconcileNodeOnline(nodes)
	db.ResolveCompositeAll(s.DB, nodes)
	lv := serverVersion()
	sha := ""
	if art, _ := s.loadAgentArtifact(); art != nil {
		sha = art.SHA
	}
	now := time.Now()
	out := make([]presenceView, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || n.NodeType == "self" {
			continue
		}
		normalizeAgentVersion(n, lv, sha)
		out = append(out, presenceFrom(n, lv, sha, now))
	}
	s.presenceSnap = out
	s.presenceAt = now
	return out
}

func presenceFrom(n *db.Node, serverVersion, latestSHA string, now time.Time) presenceView {
	p := presenceView{
		ID:                   n.ID,
		Online:               n.Online,
		LastWarning:          n.LastWarning,
		LastDisconnectReason: n.LastDisconnectReason,
		EgressNote:           n.EgressNote,
		ResourceWarning:      n.ResourceWarning,
		AgentVersion:         n.AgentVersion,
		AgentSHA:             n.AgentSHA,
		UpgradeAttention:     upgradeAttention(n, serverVersion, latestSHA, now),
		Disabled:             n.Disabled,
		DownHops:             downHopNames(n),
	}
	if n.LastSeen != nil {
		p.LastSeen = *n.LastSeen
	}
	if n.LastApplyAt.Valid {
		p.LastApplyAt = n.LastApplyAt.Int64
	}
	if n.LastError.Valid {
		p.LastError = n.LastError.String
	}
	if p.DownHops == nil {
		p.DownHops = []string{}
	}
	return p
}

func downHopNames(n *db.Node) []string {
	if n == nil || n.NodeType != "composite" {
		return nil
	}
	var names []string
	for _, h := range n.Hops {
		if h.Online == 1 && !h.Disabled {
			continue
		}
		name := h.Name
		if name == "" {
			name = fmt.Sprintf("#%d", h.NodeID)
		}
		names = append(names, name)
	}
	return names
}

func upgradeAttention(n *db.Node, serverVersion, latestSHA string, now time.Time) string {
	if n == nil {
		return ""
	}
	switch st := deriveUpgradeStatus(n, serverVersion, latestSHA, now).Status; st {
	case "error", "stuck", "rolled_back":
		return st
	default:
		return ""
	}
}
