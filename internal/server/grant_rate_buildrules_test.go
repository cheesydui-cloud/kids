package server

import (
	"database/sql"
	"testing"

	"nft/internal/db"
	"nft/internal/nft"
)

// Rate limits are retired: leftover grant / profile Mbps must not shape
// traffic or force TCP onto userspace.
func TestGrantRateLimitDoesNotShapeRules(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rl-node", "https://p", "s")
	db.GrantNode(d, uid, n.ID, 10, 0)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_limit_mbytes=10 WHERE user_id=? AND node_id=?`, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE users SET speed_limit_mbytes=5 WHERE id=?`, uid); err != nil {
		t.Fatal(err)
	}
	owned, _ := createStandaloneRuleHop(t, d, n.ID, "tcp", 0, "10.0.0.9", 9000, sql.NullInt64{Int64: uid, Valid: true})
	orphan, _ := createStandaloneRuleHop(t, d, n.ID, "tcp", 0, "10.0.0.9", 9001, sql.NullInt64{})

	ruleHops, _ := db.ActiveRuleHopsForPush(d, n.ID)
	rules := buildRules(d, ruleHops)

	var foundOwned, foundOrphan bool
	for _, r := range rules {
		switch r.RuleID {
		case owned:
			foundOwned = true
			if r.ShapeGroup != 0 || r.RateMBytes != 0 || r.BandwidthMbps != 0 {
				t.Errorf("owned rule must be unshaped after rate-limit removal, got %+v", r)
			}
			if r.Mode == nft.ModeUserspace {
				t.Errorf("owned TCP rule mode = %q, leftover rate must not force userspace", r.Mode)
			}
		case orphan:
			foundOrphan = true
			if r.ShapeGroup != 0 || r.RateMBytes != 0 || r.BandwidthMbps != 0 {
				t.Errorf("ownerless rule must be unshaped, got %+v", r)
			}
		}
	}
	if !foundOwned || !foundOrphan {
		t.Fatalf("rules missing from built set: owned=%v orphan=%v", foundOwned, foundOrphan)
	}
}

// A leftover composite grant rate must not stamp shape fields onto physical hops.
func TestGrantRateLimitDoesNotPropagateToCompositeChainHops(t *testing.T) {
	d := openDB(t)
	a, _ := db.CreateNode(d, "hk", "https://p", "ta")
	b, _ := db.CreateNode(d, "jp", "https://p", "tb")
	_ = db.UpdateNodeRelayHost(d, a.ID, "1.1.1.1")
	_ = db.UpdateNodeRelayHost(d, b.ID, "2.2.2.2")
	comp := makeComposite(t, d, "chain", a.ID, b.ID)

	uid, _ := loginAsUser(t, d, 10)
	_ = db.GrantNode(d, uid, comp.ID, 10, 0)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_limit_mbytes=10 WHERE user_id=? AND node_id=?`, uid, comp.ID); err != nil {
		t.Fatal(err)
	}

	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rl := &db.Rule{NodeID: comp.ID, OwnerID: sql.NullInt64{Int64: uid, Valid: true}, Name: "chain-rl", Proto: "tcp", ExitHost: "9.9.9.9", ExitPort: 8443}
	ruleID, err := db.CreateRule(tx, rl)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	rl.ID = ruleID
	hops := []db.HopInput{{NodeID: a.ID, Mode: "kernel"}, {NodeID: b.ID, Mode: "kernel"}}
	if _, _, _, err := db.RegenerateRule(tx, rl, hops, nil); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	for _, phys := range []*db.Node{a, b} {
		ruleHops, err := db.ActiveRuleHopsForPush(d, phys.ID)
		if err != nil {
			t.Fatal(err)
		}
		rules := buildRules(d, ruleHops)
		var found bool
		for _, r := range rules {
			if r.RuleID != ruleID {
				continue
			}
			found = true
			if r.ShapeGroup != 0 || r.RateMBytes != 0 || r.Mode != nft.ModeKernel {
				t.Errorf("node %d (%s): leftover grant must not shape hop, got group %d rate %d mode %q",
					phys.ID, phys.Name, r.ShapeGroup, r.RateMBytes, r.Mode)
			}
		}
		if !found {
			t.Fatalf("node %d (%s): rule %d missing from pushed rule set", phys.ID, phys.Name, ruleID)
		}
	}
}

// Shape fields are still data plane state: changing them must change the rev so
// reconnecting agents are not skipped by the rev short-circuit (e.g. when an
// older panel that still shaped reconnects to one that does not).
func TestComputeRevIncludesShapeFields(t *testing.T) {
	base := []nft.Rule{{Proto: "tcp", SrcPort: 1, DestIP: "1.1.1.1", DestPort: 1}}
	shaped := []nft.Rule{{Proto: "tcp", SrcPort: 1, DestIP: "1.1.1.1", DestPort: 1, ShapeGroup: 3, RateMBytes: 10}}
	if computeRev(base) == computeRev(shaped) {
		t.Fatal("rev must differ when shape fields differ")
	}
}

func mustActiveHops(t *testing.T, d *sql.DB, nodeID int64) []*db.RuleHop {
	t.Helper()
	hops, err := db.ActiveRuleHopsForPush(d, nodeID)
	if err != nil {
		t.Fatal(err)
	}
	return hops
}
