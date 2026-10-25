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

// rate_mbps shapes every owned rule on that panel node through one group,
// and only on the entry hop. Leftover columns and the profile speed do not
// add to it, and the hop mode is left alone.
func TestGrantRateMbpsShapesOwnedEntry(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rl-live", "https://p", "s")
	db.GrantNode(d, uid, n.ID, 10, 0)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_mbps=100, rate_limit_mbytes=7 WHERE user_id=? AND node_id=?`, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE users SET speed_limit_mbytes=5 WHERE id=?`, uid); err != nil {
		t.Fatal(err)
	}
	shapes, err := db.GrantShapes(d)
	if err != nil {
		t.Fatal(err)
	}
	wantID := shapes[[2]int64{uid, n.ID}].GrantID
	if wantID <= 0 {
		t.Fatal("missing grant id")
	}

	first, _ := createStandaloneRuleHop(t, d, n.ID, "tcp", 0, "10.0.0.9", 9000, sql.NullInt64{Int64: uid, Valid: true})
	second, _ := createStandaloneRuleHop(t, d, n.ID, "tcp", 0, "10.0.0.9", 9001, sql.NullInt64{Int64: uid, Valid: true})
	orphan, _ := createStandaloneRuleHop(t, d, n.ID, "tcp", 0, "10.0.0.9", 9002, sql.NullInt64{})

	rules := buildRules(d, mustActiveHops(t, d, n.ID))
	found := map[int64]bool{}
	for _, r := range rules {
		switch r.RuleID {
		case first, second:
			found[r.RuleID] = true
			if r.ShapeGroup != wantID || r.RateMBytes != 100 || r.BandwidthMbps != 0 {
				t.Errorf("rule %d shape = group %d rate %d bw %d, want group %d rate 100 bw 0",
					r.RuleID, r.ShapeGroup, r.RateMBytes, r.BandwidthMbps, wantID)
			}
			if r.Mode == nft.ModeUserspace {
				t.Errorf("rule %d mode = %q, a rate must not force userspace", r.RuleID, r.Mode)
			}
		case orphan:
			found[r.RuleID] = true
			if r.ShapeGroup != 0 || r.RateMBytes != 0 || r.BandwidthMbps != 0 {
				t.Errorf("ownerless rule must be unshaped, got %+v", r)
			}
		}
	}
	if !found[first] || !found[second] || !found[orphan] {
		t.Fatalf("rules missing: %v", found)
	}
}

func TestGrantRateMbpsShapesOnlyCompositeEntry(t *testing.T) {
	d := openDB(t)
	a, _ := db.CreateNode(d, "hk", "https://p", "ta")
	b, _ := db.CreateNode(d, "jp", "https://p", "tb")
	_ = db.UpdateNodeRelayHost(d, a.ID, "1.1.1.1")
	_ = db.UpdateNodeRelayHost(d, b.ID, "2.2.2.2")
	comp := makeComposite(t, d, "chain-rate", a.ID, b.ID)

	uid, _ := loginAsUser(t, d, 10)
	_ = db.GrantNode(d, uid, comp.ID, 10, 0)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_mbps=40, rate_limit_mbytes=9 WHERE user_id=? AND node_id=?`, uid, comp.ID); err != nil {
		t.Fatal(err)
	}
	shapes, err := db.GrantShapes(d)
	if err != nil {
		t.Fatal(err)
	}
	wantID := shapes[[2]int64{uid, comp.ID}].GrantID

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
	hops := []db.HopInput{{NodeID: a.ID, Mode: "kernel"}, {NodeID: b.ID, Mode: "userspace"}}
	if _, _, _, err := db.RegenerateRule(tx, rl, hops, nil); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	entry := buildRules(d, mustActiveHops(t, d, a.ID))
	var entryRule *nft.Rule
	for i := range entry {
		if entry[i].RuleID == ruleID {
			entryRule = &entry[i]
			break
		}
	}
	if entryRule == nil {
		t.Fatal("entry hop missing")
	}
	if entryRule.ShapeGroup != wantID || entryRule.RateMBytes != 40 || entryRule.Mode != nft.ModeKernel || entryRule.BandwidthMbps != 0 {
		t.Fatalf("entry hop = group %d rate %d mode %q bw %d", entryRule.ShapeGroup, entryRule.RateMBytes, entryRule.Mode, entryRule.BandwidthMbps)
	}

	later := buildRules(d, mustActiveHops(t, d, b.ID))
	var laterRule *nft.Rule
	for i := range later {
		if later[i].RuleID == ruleID {
			laterRule = &later[i]
			break
		}
	}
	if laterRule == nil {
		t.Fatal("later hop missing")
	}
	if laterRule.ShapeGroup != 0 || laterRule.RateMBytes != 0 || laterRule.BandwidthMbps != 0 || laterRule.Mode != nft.ModeUserspace {
		t.Fatalf("later hop must stay unshaped and keep its mode, got group %d rate %d mode %q",
			laterRule.ShapeGroup, laterRule.RateMBytes, laterRule.Mode)
	}
}

func TestGrantRateMbpsKeepsUserspaceMode(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rl-us", "https://p", "s")
	_ = db.UpdateNodeRelayHost(d, n.ID, "1.1.1.1")
	db.GrantNode(d, uid, n.ID, 10, 0)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_mbps=8 WHERE user_id=? AND node_id=?`, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	rl := &db.Rule{NodeID: n.ID, OwnerID: sql.NullInt64{Int64: uid, Valid: true}, Name: "us", Proto: "tcp", ExitHost: "9.9.9.9", ExitPort: 443}
	ruleID, err := db.CreateRule(tx, rl)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	rl.ID = ruleID
	if _, _, _, err := db.RegenerateRule(tx, rl, []db.HopInput{{NodeID: n.ID, Mode: "userspace"}}, nil); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rules := buildRules(d, mustActiveHops(t, d, n.ID))
	for _, r := range rules {
		if r.RuleID != ruleID {
			continue
		}
		if r.Mode != nft.ModeUserspace || r.RateMBytes != 8 || r.ShapeGroup <= 0 || r.BandwidthMbps != 0 {
			t.Fatalf("userspace rule = mode %q group %d rate %d bw %d", r.Mode, r.ShapeGroup, r.RateMBytes, r.BandwidthMbps)
		}
		return
	}
	t.Fatal("userspace rule missing")
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
