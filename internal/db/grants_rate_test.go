package db

import "testing"

func TestGrantRateLimitRoundTrip(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	nid := createTestNode(t, d, "rl")
	grantNode(t, d, uid, nid)

	if _, err := d.Exec(`UPDATE user_nodes SET rate_mbps=100, rate_limit_mbytes=10 WHERE user_id=? AND node_id=?`, uid, nid); err != nil {
		t.Fatal(err)
	}
	g, err := GetNodeGrant(d, uid, nid)
	if err != nil {
		t.Fatal(err)
	}
	if g.RateMbps != 100 {
		t.Fatalf("rate_mbps = %d, want 100", g.RateMbps)
	}
	if g.RateLimitMBytes != 10 {
		t.Fatalf("leftover rate_limit_mbytes = %d, want 10", g.RateLimitMBytes)
	}

	shapes, err := GrantShapes(d)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := shapes[[2]int64{uid, nid}]
	if !ok || s.RateMbps != 100 || s.GrantID <= 0 {
		t.Fatalf("shape = %+v ok=%v, want rate 100 with positive grant id", s, ok)
	}

	// GrantNode upsert must not touch the rate and must keep the rowid stable
	// (the rowid is the shaping group id; churning it would orphan connmarks).
	if err := GrantNode(d, uid, nid, 5, 0); err != nil {
		t.Fatal(err)
	}
	shapes2, _ := GrantShapes(d)
	s2 := shapes2[[2]int64{uid, nid}]
	if s2.GrantID != s.GrantID || s2.RateMbps != 100 {
		t.Fatalf("after upsert shape = %+v, want unchanged %+v", s2, s)
	}
	g2, err := GetNodeGrant(d, uid, nid)
	if err != nil {
		t.Fatal(err)
	}
	if g2.RateLimitMBytes != 10 {
		t.Fatalf("upsert cleared leftover rate_limit_mbytes, got %d", g2.RateLimitMBytes)
	}
}

func TestGrantShapesIgnoresLeftoverRate(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	nid := createTestNode(t, d, "rl-old")
	grantNode(t, d, uid, nid)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_limit_mbytes=10 WHERE user_id=? AND node_id=?`, uid, nid); err != nil {
		t.Fatal(err)
	}
	shapes, err := GrantShapes(d)
	if err != nil {
		t.Fatal(err)
	}
	s := shapes[[2]int64{uid, nid}]
	if s.RateMbps != 0 {
		t.Fatalf("leftover rate_limit_mbytes leaked into GrantShapes: %+v", s)
	}
}

func TestGrantShapesIncludesUnlimited(t *testing.T) {
	d := openTestDB(t)
	uid := createTestUser(t, d)
	nid := createTestNode(t, d, "rl0")
	grantNode(t, d, uid, nid)

	shapes, err := GrantShapes(d)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := shapes[[2]int64{uid, nid}]
	if !ok {
		t.Fatal("rate 0 grant must still appear in GrantShapes")
	}
	if s.RateMbps != 0 {
		t.Fatalf("rate = %d, want 0", s.RateMbps)
	}
	if s.GrantID <= 0 {
		t.Fatalf("grant id = %d, want positive", s.GrantID)
	}
}
