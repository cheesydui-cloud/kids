package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"nft/internal/db"
)

// ListNodesForUser interpolates nodeCols into its SELECT but scans by an
// inline argument list; the two must stay in lockstep or every granted-node
// listing silently fails. Guard that the scan count matches the column count.
func TestListNodesForUserAfterGrant(t *testing.T) {
	d := openDB(t)
	n, err := db.CreateNode(d, "edge", "https://p", "tok")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := HashPassword("pw")
	uid, err := db.CreateUser(d, "u1", hash, "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.GrantNode(d, uid, n.ID, 5, 0); err != nil {
		t.Fatal(err)
	}
	nodes, grants, err := db.ListNodesForUser(d, uid)
	if err != nil {
		t.Fatalf("ListNodesForUser: %v", err)
	}
	if len(nodes) != 1 || len(grants) != 1 || nodes[0].ID != n.ID {
		t.Fatalf("expected 1 granted node, got nodes=%d grants=%d", len(nodes), len(grants))
	}
}

func TestNodeUpgradeColumnsRoundTrip(t *testing.T) {
	d := openDB(t)
	n, err := db.CreateNode(d, "edge", "https://p", "tok")
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.GetNode(d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastUpgradeAt.Valid {
		t.Fatalf("fresh node should have null last_upgrade_at, got %+v", got.LastUpgradeAt)
	}

	if err := db.RecordUpgradeResult(d, n.ID, "v1.2.3", "acked", ""); err != nil {
		t.Fatal(err)
	}
	got, err = db.GetNode(d, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastUpgradeAt.Valid || got.LastUpgradeVersion != "v1.2.3" || got.LastUpgradeStatus != "acked" || got.LastUpgradeError != "" {
		t.Fatalf("after acked record: %+v", got)
	}

	if err := db.RecordUpgradeResult(d, n.ID, "v1.2.3", "error", "节点未连接"); err != nil {
		t.Fatal(err)
	}
	got, _ = db.GetNode(d, n.ID)
	if got.LastUpgradeStatus != "error" || got.LastUpgradeError != "节点未连接" {
		t.Fatalf("after error record: status=%q err=%q", got.LastUpgradeStatus, got.LastUpgradeError)
	}
}

func TestApiUpgradeNodeRecordsError(t *testing.T) {
	d := openDB(t)
	n, err := db.CreateNode(d, "edge", "https://p", "tok")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := HashPassword("pw")
	aid, err := db.CreateUser(d, "admin1", hash, "admin")
	if err != nil {
		t.Fatal(err)
	}
	cookieTok, err := db.CreateSession(d, aid, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := &http.Cookie{Name: sessionCookie, Value: cookieTok}

	s := newServer(t, d)

	// Warm the agent artifact cache so the push reaches SendUpgrade without
	// hitting GitHub (a "dev" test build has no matching release).
	agentArtMu.Lock()
	agentArtCache = &agentArtifact{Version: serverVersion(), SHA: "deadbeef", Data: []byte("agent-binary")}
	agentArtMu.Unlock()
	defer func() {
		agentArtMu.Lock()
		agentArtCache = nil
		agentArtByArch = map[string]*agentArtifact{}
		agentArtMu.Unlock()
	}()

	// Node not connected -> SendUpgrade fails -> must be recorded as error.
	req := newTestRequest("POST", "/api/nodes/"+strconv.FormatInt(n.ID, 10)+"/upgrade", bytes.NewReader([]byte("{}")))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	got, _ := db.GetNode(d, n.ID)
	if got.LastUpgradeStatus != "error" || got.LastUpgradeError == "" {
		t.Fatalf("expected recorded error after failed upgrade, got status=%q err=%q (http=%d)", got.LastUpgradeStatus, got.LastUpgradeError, rec.Code)
	}

	// apiGetNode must expose a derived upgrade object.
	req2 := newTestRequest("GET", "/api/nodes/"+strconv.FormatInt(n.ID, 10), nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("get node: http=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var resp struct {
		Upgrade upgradeView `json:"upgrade"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Upgrade.Status != "error" {
		t.Fatalf("apiGetNode upgrade.status=%q want error", resp.Upgrade.Status)
	}
}

func TestApiUpgradeAllSkipsCurrentAndDoesNotWait(t *testing.T) {
	d := openDB(t)
	current, err := db.CreateNode(d, "current", "https://p", "tok-c")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := db.CreateNode(d, "stale", "https://p", "tok-s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE nodes SET agent_sha=?, agent_version=? WHERE id=?`, "deadbeef", serverVersion(), current.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE nodes SET agent_sha=?, agent_version=? WHERE id=?`, "oldsha", "v0.0.1", stale.ID); err != nil {
		t.Fatal(err)
	}

	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	agentArtMu.Lock()
	agentArtCache = &agentArtifact{Version: serverVersion(), SHA: "deadbeef", Data: []byte("agent-binary")}
	agentArtByArch["amd64"] = agentArtCache
	agentArtMu.Unlock()
	defer func() {
		agentArtMu.Lock()
		agentArtCache = nil
		agentArtByArch = map[string]*agentArtifact{}
		agentArtMu.Unlock()
	}()

	start := time.Now()
	req := newTestRequest("POST", "/api/nodes/upgrade-all", bytes.NewReader([]byte("{}")))
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upgrade-all http=%d body=%s", rec.Code, rec.Body.String())
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("upgrade-all blocked for %s; must not wait for agent ACK", time.Since(start))
	}
	var resp struct {
		Pushed  int `json:"pushed"`
		Skipped int `json:"skipped"`
		Failed  int `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Skipped < 1 {
		t.Fatalf("current node should be skipped, got %+v body=%s", resp, rec.Body.String())
	}
	if resp.Failed < 1 {
		t.Fatalf("offline stale node should fail immediately, got %+v", resp)
	}
	got, _ := db.GetNode(d, stale.ID)
	if got.LastUpgradeStatus != "error" {
		t.Fatalf("offline stale status=%q want error", got.LastUpgradeStatus)
	}
	gotCur, _ := db.GetNode(d, current.ID)
	if gotCur.LastUpgradeStatus != "" && gotCur.LastUpgradeAt.Valid {
		t.Fatalf("current node should not record a new upgrade, status=%q", gotCur.LastUpgradeStatus)
	}
}
