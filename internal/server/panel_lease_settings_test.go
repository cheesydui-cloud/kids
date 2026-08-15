package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nft/internal/db"
)

func TestParsePanelLeaseHours(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 24},
		{"0", 24},
		{"-1", 24},
		{"abc", 24},
		{"1", 1},
		{"24", 24},
		{"48", 48},
		{"168", 168},
		{"169", 168},
	}
	for _, c := range cases {
		if got := parsePanelLeaseHours(c.in); got != c.want {
			t.Errorf("parsePanelLeaseHours(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestSettingsPanelLeaseHoursRoundtrip(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	req := newTestRequest("GET", "/api/settings", nil)
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["panel_lease_hours"] != float64(24) {
		t.Fatalf("default lease hours = %v, want 24", got["panel_lease_hours"])
	}

	body, _ := json.Marshal(map[string]any{
		"panel_url":         "http://127.0.0.1:7788",
		"panel_lease_hours": 48,
	})
	req = newTestRequest("POST", "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := db.GetSetting(d, "panel_lease_hours")
	if stored != "48" {
		t.Fatalf("stored=%q", stored)
	}

	body, _ = json.Marshal(map[string]any{
		"panel_url":         "http://127.0.0.1:7788",
		"panel_lease_hours": 200,
	})
	req = newTestRequest("POST", "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize lease: %d %s", rec.Code, rec.Body.String())
	}
}
