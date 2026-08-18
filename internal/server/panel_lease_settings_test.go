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

func TestNormalizeMonitorURL(t *testing.T) {
	ok, err := normalizeMonitorURL(" https://komari.example.com/ ")
	if err != nil || ok != "https://komari.example.com/" {
		t.Fatalf("https: %q %v", ok, err)
	}
	ok, err = normalizeMonitorURL("http://127.0.0.1:25774")
	if err != nil || ok != "http://127.0.0.1:25774" {
		t.Fatalf("http: %q %v", ok, err)
	}
	ok, err = normalizeMonitorURL("  ")
	if err != nil || ok != "" {
		t.Fatalf("blank: %q %v", ok, err)
	}
	if _, err := normalizeMonitorURL("javascript:alert(1)"); err == nil {
		t.Fatal("javascript must fail")
	}
	if _, err := normalizeMonitorURL("komari.example.com"); err == nil {
		t.Fatal("bare host must fail")
	}
}

func TestSettingsMonitorURLRoundtripAndMeScope(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	body, _ := json.Marshal(map[string]any{
		"panel_url":   "http://127.0.0.1:7788",
		"monitor_url": "https://komari.example.com",
	})
	req := newTestRequest("POST", "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := db.GetSetting(d, "monitor_url")
	if stored != "https://komari.example.com" {
		t.Fatalf("stored=%q", stored)
	}

	req = newTestRequest("GET", "/api/settings", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	var settings map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["monitor_url"] != "https://komari.example.com" {
		t.Fatalf("settings monitor_url=%v", settings["monitor_url"])
	}

	req = newTestRequest("GET", "/api/me", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	var me map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me["monitor_url"] != "https://komari.example.com" {
		t.Fatalf("admin /me missing monitor_url: %v", me["monitor_url"])
	}

	_, userCookie := loginAsUser(t, d, 1)
	req = newTestRequest("GET", "/api/me", nil)
	req.AddCookie(userCookie)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	me = map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if _, ok := me["monitor_url"]; ok {
		t.Fatalf("user /me must not include monitor_url: %v", me["monitor_url"])
	}

	req = newTestRequest("GET", "/api/branding", nil)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	var brand map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &brand); err != nil {
		t.Fatal(err)
	}
	if _, ok := brand["monitor_url"]; ok {
		t.Fatal("public branding must not include monitor_url")
	}

	body, _ = json.Marshal(map[string]any{
		"panel_url":   "http://127.0.0.1:7788",
		"monitor_url": "javascript:alert(1)",
	})
	req = newTestRequest("POST", "/api/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad scheme: %d %s", rec.Code, rec.Body.String())
	}
}
