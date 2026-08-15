package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nft/internal/db"
	"nft/internal/landing"
)

func seedSubUser(t *testing.T, d *sql.DB) (uid int64, cookie *http.Cookie, entryNode *db.Node) {
	t.Helper()
	uid, cookie = loginAsUser(t, d, 10)
	n, err := db.CreateNode(d, "gz", "https://p", "sekret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateNodeRelayHost(d, n.ID, "relay.example"); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantNode(d, uid, n.ID, 5, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.SyncUserLandingExits(d, uid, []db.LandingExitInput{
		{Host: "1.2.3.4", Port: 443, Name: "HK", Protocol: "vless", URI: "vless://uuid@1.2.3.4:443?security=tls&sni=a.com#HK"},
		{Host: "5.6.7.8", Port: 8443, Name: "JP", Protocol: "trojan", URI: "trojan://pass@5.6.7.8:8443?sni=b.com#JP"},
	}, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(d, "node_roles", `{"trojan:5.6.7.8:8443":2}`); err != nil {
		t.Fatal(err)
	}
	createNamedRule(t, d, uid, n.ID, "线路A", "1.2.3.4", 443)
	createNamedRule(t, d, uid, n.ID, "自定义", "9.9.9.9", 1080)
	return uid, cookie, n
}

func createNamedRule(t *testing.T, d *sql.DB, ownerID, nodeID int64, name, exitHost string, exitPort int) int64 {
	t.Helper()
	rl := &db.Rule{
		NodeID:   nodeID,
		OwnerID:  sql.NullInt64{Int64: ownerID, Valid: true},
		Name:     name,
		Proto:    "tcp",
		ExitHost: exitHost,
		ExitPort: exitPort,
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateRule(tx, rl)
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	rl.ID = id
	if _, _, _, err := db.RegenerateRule(tx, rl, []db.HopInput{{NodeID: nodeID}}, nil); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSubscribeCollectsRelayAndDirect(t *testing.T) {
	d := openDB(t)
	uid, cookie, _ := seedSubUser(t, d)
	s := newServer(t, d)

	req := newTestRequest("GET", "/api/my/subscribe", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token    string `json:"token"`
		URIURL   string `json:"uri_url"`
		ClashURL string `json:"clash_url"`
		Items    []struct {
			Kind     string `json:"kind"`
			Name     string `json:"name"`
			URI      string `json:"uri"`
			RuleName string `json:"rule_name"`
		} `json:"items"`
		Skipped []struct {
			Reason string `json:"reason"`
			Detail string `json:"detail"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Token == "" || !strings.Contains(body.URIURL, "/api/v1/sub?token=") {
		t.Fatalf("urls = %+v", body)
	}
	var relay, direct int
	for _, it := range body.Items {
		switch it.Kind {
		case "relay":
			relay++
			if !strings.Contains(it.URI, "relay.example:") {
				t.Errorf("relay uri should use entry host, got %s", it.URI)
			}
			if strings.Contains(it.URI, "1.2.3.4") {
				t.Errorf("relay uri still points at landing: %s", it.URI)
			}
		case "direct":
			direct++
			if !strings.Contains(it.URI, "5.6.7.8:8443") {
				t.Errorf("direct uri should keep landing endpoint, got %s", it.URI)
			}
			if !strings.Contains(it.Name, "直连") {
				t.Errorf("direct name = %q", it.Name)
			}
		}
	}
	if relay != 1 || direct != 1 {
		t.Fatalf("items relay=%d direct=%d body=%s", relay, direct, rec.Body.String())
	}
	foundCustom := false
	for _, sk := range body.Skipped {
		if sk.Reason == "custom" && sk.Detail == "自定义" {
			foundCustom = true
		}
	}
	if !foundCustom {
		t.Fatalf("custom rule should be skipped: %+v", body.Skipped)
	}

	tok := body.Token
	pub := newTestRequest("GET", "/api/v1/sub?token="+tok, nil)
	prec := httptest.NewRecorder()
	s.Router().ServeHTTP(prec, pub)
	if prec.Code != http.StatusOK {
		t.Fatalf("public sub: %d %s", prec.Code, prec.Body.String())
	}
	uris := landing.DecodeSubscription(prec.Body.Bytes())
	if len(uris) != 2 {
		t.Fatalf("decoded uris = %#v", uris)
	}

	cyaml := newTestRequest("GET", "/api/v1/clash.yaml?token="+tok, nil)
	crec := httptest.NewRecorder()
	s.Router().ServeHTTP(crec, cyaml)
	if crec.Code != http.StatusOK {
		t.Fatalf("clash: %d %s", crec.Code, crec.Body.String())
	}
	yml := crec.Body.String()
	if !strings.Contains(yml, "type: vless") || !strings.Contains(yml, "type: trojan") {
		t.Fatalf("clash yaml missing proxies:\n%s", yml)
	}
	if !strings.Contains(yml, "relay.example") || !strings.Contains(yml, "5.6.7.8") {
		t.Fatalf("clash yaml endpoints:\n%s", yml)
	}

	_ = uid
}

func TestSubscribeRotateInvalidatesOldURL(t *testing.T) {
	d := openDB(t)
	_, cookie, _ := seedSubUser(t, d)
	s := newServer(t, d)

	req := newTestRequest("GET", "/api/my/subscribe", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	var first struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &first)
	if first.Token == "" {
		t.Fatal("no token")
	}

	rot := newTestRequest("POST", "/api/my/subscribe/rotate", nil)
	rot.AddCookie(cookie)
	rrec := httptest.NewRecorder()
	s.Router().ServeHTTP(rrec, rot)
	if rrec.Code != http.StatusOK {
		t.Fatalf("rotate: %d %s", rrec.Code, rrec.Body.String())
	}
	var second struct {
		Token string `json:"token"`
	}
	json.Unmarshal(rrec.Body.Bytes(), &second)
	if second.Token == "" || second.Token == first.Token {
		t.Fatalf("token not rotated: %q -> %q", first.Token, second.Token)
	}

	old := newTestRequest("GET", "/api/v1/sub?token="+first.Token, nil)
	orec := httptest.NewRecorder()
	s.Router().ServeHTTP(orec, old)
	if orec.Code != http.StatusUnauthorized {
		t.Fatalf("old token still works: %d %s", orec.Code, orec.Body.String())
	}

	fresh := newTestRequest("GET", "/api/v1/sub?token="+second.Token, nil)
	frec := httptest.NewRecorder()
	s.Router().ServeHTTP(frec, fresh)
	if frec.Code != http.StatusOK {
		t.Fatalf("new token rejected: %d %s", frec.Code, frec.Body.String())
	}
}

func TestSubscribePacksEveryLandingRule(t *testing.T) {
	d := openDB(t)
	uid, cookie, n := seedSubUser(t, d)
	if _, _, err := db.SyncUserLandingExits(d, uid, []db.LandingExitInput{
		{Host: "1.2.3.4", Port: 443, Name: "HK", Protocol: "vless", URI: "vless://uuid@1.2.3.4:443?security=tls&sni=a.com#HK"},
		{Host: "5.6.7.8", Port: 8443, Name: "JP", Protocol: "trojan", URI: "trojan://pass@5.6.7.8:8443?sni=b.com#JP"},
		{Host: "8.8.8.8", Port: 443, Name: "SG", Protocol: "ss", URI: "ss://YWVzLTI1Ni1nY206cGFzcw==@8.8.8.8:443#SG"},
	}, "", ""); err != nil {
		t.Fatal(err)
	}
	// JP stays ROLE_DIRECT; SG is a second landing-matched rule.
	if err := db.SetSetting(d, "node_roles", `{"trojan:5.6.7.8:8443":2}`); err != nil {
		t.Fatal(err)
	}
	createNamedRule(t, d, uid, n.ID, "线路B", "8.8.8.8", 443)
	s := newServer(t, d)

	req := newTestRequest("GET", "/api/my/subscribe", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("subscribe: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token string `json:"token"`
		Items []struct {
			Kind     string `json:"kind"`
			URI      string `json:"uri"`
			RuleName string `json:"rule_name"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var relay, direct int
	rules := map[string]bool{}
	for _, it := range body.Items {
		switch it.Kind {
		case "relay":
			relay++
			rules[it.RuleName] = true
			if !strings.Contains(it.URI, "relay.example:") {
				t.Errorf("relay uri should use entry host, got %s", it.URI)
			}
		case "direct":
			direct++
		}
	}
	if relay != 2 || direct != 1 {
		t.Fatalf("items relay=%d direct=%d body=%s", relay, direct, rec.Body.String())
	}
	if !rules["线路A"] || !rules["线路B"] {
		t.Fatalf("missing landing rules in items: %+v", rules)
	}

	cyaml := newTestRequest("GET", "/api/v1/clash.yaml?token="+body.Token, nil)
	crec := httptest.NewRecorder()
	s.Router().ServeHTTP(crec, cyaml)
	if crec.Code != http.StatusOK {
		t.Fatalf("clash: %d %s", crec.Code, crec.Body.String())
	}
	yml := crec.Body.String()
	if strings.Count(yml, "server: relay.example") < 2 {
		t.Fatalf("clash yaml should pack both relay entries:\n%s", yml)
	}
	if !strings.Contains(yml, "type: vless") || !strings.Contains(yml, "type: ss") || !strings.Contains(yml, "type: trojan") {
		t.Fatalf("clash yaml missing proxy types:\n%s", yml)
	}
	if !strings.Contains(yml, "5.6.7.8") {
		t.Fatalf("clash yaml missing direct landing:\n%s", yml)
	}
}

func TestSubscribeRejectsMissingToken(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	req := newTestRequest("GET", "/api/v1/sub", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d", rec.Code)
	}
}
