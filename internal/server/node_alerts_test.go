package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nft/internal/db"
)

func seenAt(unix int64) *int64 { return &unix }

func TestPlanNodeAlertOfflineAndRecovery(t *testing.T) {
	now := int64(1_000_000)
	n := &db.Node{ID: 7, Name: "hk-1", RelayHost: "1.2.3.4", Online: 0, LastSeen: seenAt(now - 30)}
	next, events, drop := planNodeAlert(n, db.NodeAlertState{}, now)
	if drop || len(events) != 0 {
		t.Fatalf("30s offline should wait, drop=%v events=%d", drop, len(events))
	}
	if next.OfflineSince != now-30 {
		t.Fatalf("clock=%d", next.OfflineSince)
	}

	n.LastSeen = seenAt(now - 90)
	next, events, drop = planNodeAlert(n, db.NodeAlertState{}, now)
	if drop || len(events) != 1 || events[0].Event != "offline" || !next.OfflineAlerted {
		t.Fatalf("90s should alert offline: %+v %+v", next, events)
	}
	if !strings.Contains(events[0].Text, "hk-1") || !strings.Contains(events[0].Text, "1.2.3.4") {
		t.Fatalf("text=%q", events[0].Text)
	}

	again, events, _ := planNodeAlert(n, next, now+10)
	if len(events) != 0 || !again.OfflineAlerted {
		t.Fatalf("already alerted should stay quiet: %+v %d", again, len(events))
	}

	n.Online = 1
	back, events, _ := planNodeAlert(n, next, now+20)
	if len(events) != 1 || events[0].Event != "online" || back.OfflineAlerted || back.OfflineSince != 0 {
		t.Fatalf("recovery: %+v %+v", back, events)
	}

	n.Online = 0
	n.LastSeen = seenAt(now - 10)
	brief, events, _ := planNodeAlert(n, db.NodeAlertState{OfflineSince: now - 10}, now)
	if len(events) != 0 {
		t.Fatalf("under a minute must not alert: %+v", events)
	}
	n.Online = 1
	cleared, events, _ := planNodeAlert(n, brief, now+5)
	if len(events) != 0 || cleared.OfflineSince != 0 {
		t.Fatalf("short blip must not announce recovery: %+v %d", cleared, len(events))
	}
}

func TestPlanNodeAlertSyncErrorOnce(t *testing.T) {
	now := int64(2_000_000)
	n := &db.Node{ID: 3, Name: "sg", Online: 1, LastError: sql.NullString{String: "nft apply failed\nmore", Valid: true}}
	next, events, drop := planNodeAlert(n, db.NodeAlertState{}, now)
	if drop || len(events) != 1 || events[0].Event != "sync_error" || events[0].Detail != "nft apply failed" {
		t.Fatalf("first error: %+v %+v", next, events)
	}
	again, events, _ := planNodeAlert(n, next, now+15)
	if len(events) != 0 {
		t.Fatalf("same error should not repeat: %+v", events)
	}
	n.LastError = sql.NullString{}
	cleared, events, _ := planNodeAlert(n, again, now+30)
	if len(events) != 0 || cleared.LastError != "" {
		t.Fatalf("cleared error: %+v %d", cleared, len(events))
	}
}

func TestPlanNodeAlertSkipsDisabledAndComposite(t *testing.T) {
	now := int64(3_000_000)
	n := &db.Node{ID: 1, Disabled: true, Online: 0, LastSeen: seenAt(now - 600)}
	_, events, drop := planNodeAlert(n, db.NodeAlertState{NodeID: 1, OfflineAlerted: true}, now)
	if !drop || len(events) != 0 {
		t.Fatalf("disabled: drop=%v events=%d", drop, len(events))
	}
	n = &db.Node{ID: 2, NodeType: "composite", Online: 0}
	_, events, drop = planNodeAlert(n, db.NodeAlertState{}, now)
	if !drop || len(events) != 0 {
		t.Fatalf("composite: drop=%v events=%d", drop, len(events))
	}
}

func TestPostAlertWebhook(t *testing.T) {
	var got nodeAlert
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Error(err)
		}
		if strings.Contains(string(body), "bot") {
			t.Error("webhook body must not carry a bot token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	ev := makeAlert("offline", &db.Node{ID: 9, Name: "jp"}, "10.0.0.9", "", time.Now().Unix())
	if err := postNodeAlerts(context.Background(), srv.URL, "", "", []nodeAlert{ev}); err != nil {
		t.Fatal(err)
	}
	if got.Event != "offline" || got.NodeID != 9 || got.Text == "" {
		t.Fatalf("%+v", got)
	}
}

func TestAlertSettingsHideToken(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	token := "123456789:AAHexampletokenvalue1"
	body, _ := json.Marshal(map[string]any{
		"panel_url":              "http://127.0.0.1:7788",
		"alert_webhook_url":      "https://hooks.example.com/line",
		"alert_telegram_token":   token,
		"alert_telegram_chat_id": "-100123",
	})
	req := newTestRequest("POST", "/api/settings", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := db.GetSetting(d, "alert_telegram_bot_token")
	if stored != token {
		t.Fatalf("stored token mismatch")
	}
	req = newTestRequest("GET", "/api/settings", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), token) {
		t.Fatal("settings response echoed the bot token")
	}
	var settings map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["alert_webhook_url"] != "https://hooks.example.com/line" {
		t.Fatalf("webhook=%v", settings["alert_webhook_url"])
	}
	if settings["alert_telegram_configured"] != true {
		t.Fatalf("configured=%v", settings["alert_telegram_configured"])
	}
	if settings["alert_telegram_chat_id"] != "-100123" {
		t.Fatalf("chat=%v", settings["alert_telegram_chat_id"])
	}

	body, _ = json.Marshal(map[string]any{
		"panel_url":         "http://127.0.0.1:7788",
		"alert_webhook_url": "javascript:alert(1)",
	})
	req = newTestRequest("POST", "/api/settings", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad webhook: %d %s", rec.Code, rec.Body.String())
	}
}

func TestProbeNodeListRequiresIDs(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	req := newTestRequest("POST", "/api/nodes/probe-latency", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty: %d %s", rec.Code, rec.Body.String())
	}

	n, err := db.CreateNode(d, "solo", "", "")
	if err != nil {
		t.Fatal(err)
	}
	req = newTestRequest("POST", "/api/nodes/probe-latency", strings.NewReader(`{"ids":[`+itoa(n.ID)+`]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("probe: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []listProbeItem `json:"items"`
		TTL   int             `json:"ttl_sec"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.TTL != 180 || len(resp.Items) != 1 || resp.Items[0].OK || resp.Items[0].Error == "" {
		t.Fatalf("%+v", resp)
	}
}

func TestNodeImpactCountsRulesAndOwners(t *testing.T) {
	d := openDB(t)
	n1, err := db.CreateNode(d, "entry", "", "")
	if err != nil {
		t.Fatal(err)
	}
	n2, err := db.CreateNode(d, "exit", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE nodes SET relay_host=? WHERE id=?`, "10.0.0.1", n1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE nodes SET relay_host=? WHERE id=?`, "10.0.0.2", n2.ID); err != nil {
		t.Fatal(err)
	}
	uid, err := db.CreateUser(d, "owner-a", "x", "user")
	if err != nil {
		t.Fatal(err)
	}
	createTestRuleWithHops(t, d, uid, n1.ID, n2.ID)
	impact, err := db.NodeImpactByNode(d)
	if err != nil {
		t.Fatal(err)
	}
	if impact[n1.ID].Rules != 1 || impact[n1.ID].Users != 1 {
		t.Fatalf("entry impact=%+v", impact[n1.ID])
	}
	if impact[n2.ID].Rules != 1 || impact[n2.ID].Users != 1 {
		t.Fatalf("exit impact=%+v", impact[n2.ID])
	}
}
