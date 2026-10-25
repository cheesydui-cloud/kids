package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nft/internal/db"
	"nft/internal/nft"
)

func TestAPISetPerNodeRateLimitRemoved(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rlnode", "", "")
	db.GrantNode(d, uid, n.ID, 10, 0)
	s := newServer(t, d)
	adminCookie := loginAsAdmin(t, d)

	body, _ := json.Marshal(map[string]any{"rate_limit_mbytes": 10})
	req := newTestRequest("POST", "/api/users/"+itoa(uid)+"/nodes/"+itoa(n.ID)+"/rate-limit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if ct := rec.Header().Get("Content-Type"); ct == "application/json" {
		t.Fatalf("want removed rate-limit endpoint to fall through to non-JSON response, got Content-Type %q body %s", ct, rec.Body.String())
	}

	g, _ := db.GetNodeGrant(d, uid, n.ID)
	if g.RateLimitMBytes != 0 {
		t.Fatalf("removed endpoint must not write leftover Mbps, got %d", g.RateLimitMBytes)
	}
}

func TestAPIRuleBandwidthEndpointRemoved(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	adminCookie := loginAsAdmin(t, d)

	req := newTestRequest("POST", "/api/rules/1/bandwidth", bytes.NewReader([]byte(`{"bandwidth_mbps":5}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	// The router's catch-all serves the SPA index for any unmatched path with
	// status 200, so an absent JSON handler cannot be told apart from a live
	// one by status code alone; the response Content-Type is the reliable
	// signal that no API handler processed the request.
	if ct := rec.Header().Get("Content-Type"); ct == "application/json" {
		t.Fatalf("want removed endpoint to fall through to non-JSON response, got Content-Type %q body %s", ct, rec.Body.String())
	}
}

func TestAPIBatchApplyGrantsIgnoresRateLimit(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rlbatch", "", "")
	s := newServer(t, d)
	adminCookie := loginAsAdmin(t, d)

	payload := map[string]any{
		"user_ids": []int64{uid},
		"grants": []map[string]any{
			{"node_name": "rlbatch", "max_forwards": 5, "traffic_quota_bytes": 0, "rate_limit_mbytes": 7, "rate_mbps": 80},
		},
	}
	body, _ := json.Marshal(payload)
	req := newTestRequest("POST", "/api/grants/batch-apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	g, err := db.GetNodeGrant(d, uid, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.RateLimitMBytes != 0 {
		t.Fatalf("batch-apply must ignore leftover rate_limit_mbytes, got %d", g.RateLimitMBytes)
	}
	if g.MaxForwards != 5 {
		t.Fatalf("want max_forwards 5, got %d", g.MaxForwards)
	}
	if g.RateMbps != 0 {
		t.Fatalf("batch-apply must ignore rate_mbps, got %d", g.RateMbps)
	}
}

func TestAPISetPerNodeRate(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rlset", "", "")
	db.GrantNode(d, uid, n.ID, 10, 0)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_limit_mbytes=4 WHERE user_id=? AND node_id=?`, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	owned, _ := createStandaloneRuleHop(t, d, n.ID, "tcp", 0, "10.0.0.9", 9000, sql.NullInt64{Int64: uid, Valid: true})
	s := newServer(t, d)
	adminCookie := loginAsAdmin(t, d)

	body, _ := json.Marshal(map[string]any{"rate_mbps": 100})
	req := newTestRequest("POST", "/api/users/"+itoa(uid)+"/nodes/"+itoa(n.ID)+"/rate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	g, err := db.GetNodeGrant(d, uid, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.RateMbps != 100 {
		t.Fatalf("rate_mbps = %d, want 100", g.RateMbps)
	}
	if g.RateLimitMBytes != 4 {
		t.Fatalf("live write must not touch leftover rate_limit_mbytes, got %d", g.RateLimitMBytes)
	}

	rules := buildRules(d, mustActiveHops(t, d, n.ID))
	for _, r := range rules {
		if r.RuleID != owned {
			continue
		}
		if r.ShapeGroup <= 0 || r.RateMBytes != 100 || r.Mode == nft.ModeUserspace {
			t.Fatalf("dispatched shape = group %d rate %d mode %q", r.ShapeGroup, r.RateMBytes, r.Mode)
		}
		return
	}
	t.Fatal("owned rule missing after rate save")
}

func TestAPISetPerNodeRateRejectsNegative(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rlneg", "", "")
	db.GrantNode(d, uid, n.ID, 10, 0)
	s := newServer(t, d)
	adminCookie := loginAsAdmin(t, d)

	body, _ := json.Marshal(map[string]any{"rate_mbps": -1})
	req := newTestRequest("POST", "/api/users/"+itoa(uid)+"/nodes/"+itoa(n.ID)+"/rate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	g, _ := db.GetNodeGrant(d, uid, n.ID)
	if g.RateMbps != 0 {
		t.Fatalf("negative rate wrote %d", g.RateMbps)
	}
}

func TestAPISetPerNodeRateRequiresGrant(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rlmiss", "", "")
	s := newServer(t, d)
	adminCookie := loginAsAdmin(t, d)

	body, _ := json.Marshal(map[string]any{"rate_mbps": 10})
	req := newTestRequest("POST", "/api/users/"+itoa(uid)+"/nodes/"+itoa(n.ID)+"/rate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAPIGetUserExposesRateMbps(t *testing.T) {
	d := openDB(t)
	uid, _ := loginAsUser(t, d, 10)
	n, _ := db.CreateNode(d, "rlget", "", "")
	db.GrantNode(d, uid, n.ID, 10, 0)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_mbps=80 WHERE user_id=? AND node_id=?`, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	s := newServer(t, d)
	adminCookie := loginAsAdmin(t, d)

	req := newTestRequest("GET", "/api/users/"+itoa(uid), nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Grants []struct {
			NodeID   int64 `json:"node_id"`
			RateMbps int64 `json:"rate_mbps"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, g := range body.Grants {
		if g.NodeID == n.ID {
			if g.RateMbps != 80 {
				t.Fatalf("grant rate_mbps = %d, want 80", g.RateMbps)
			}
			return
		}
	}
	t.Fatalf("grant for node %d missing: %+v", n.ID, body.Grants)
}

func TestSubscribeShowsGrantRate(t *testing.T) {
	d := openDB(t)
	uid, cookie, n := seedSubUser(t, d)
	if _, err := d.Exec(`UPDATE user_nodes SET rate_mbps=100, rate_limit_mbytes=7 WHERE user_id=? AND node_id=?`, uid, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE users SET speed_limit_mbytes=5 WHERE id=?`, uid); err != nil {
		t.Fatal(err)
	}
	s := newServer(t, d)
	req := newTestRequest("GET", "/api/my/subscribe", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			Kind     string `json:"kind"`
			RateMbps int    `json:"rate_mbps"`
		} `json:"items"`
		Rules []struct {
			Name     string `json:"name"`
			RateMbps int64  `json:"rate_mbps"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var relay, direct int
	for _, it := range body.Items {
		switch it.Kind {
		case "relay":
			relay++
			if it.RateMbps != 100 {
				t.Errorf("relay rate_mbps = %d, want 100", it.RateMbps)
			}
		case "direct":
			direct++
			if it.RateMbps != 0 {
				t.Errorf("direct landing must not show a grant cap, got %d", it.RateMbps)
			}
		}
	}
	if relay != 1 || direct != 1 {
		t.Fatalf("items relay=%d direct=%d", relay, direct)
	}
	found := false
	for _, rl := range body.Rules {
		if rl.Name == "自定义" {
			found = true
			if rl.RateMbps != 100 {
				t.Errorf("hidden rule rate_mbps = %d, want 100", rl.RateMbps)
			}
		}
	}
	if !found {
		t.Fatalf("custom rule missing from rules: %+v", body.Rules)
	}
}
