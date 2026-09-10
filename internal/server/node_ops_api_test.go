package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nft/internal/db"
)

func TestNodeOpsAPIAndTenantIsolation(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	node, err := db.CreateNode(d, "ops-api", "https://panel", "tok")
	if err != nil {
		t.Fatal(err)
	}

	post := func(path string, cookie *http.Cookie, payload any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		req := newTestRequest("POST", path, bytes.NewReader(body))
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		return rec
	}

	rec := post("/api/nodes/folders", admin, map[string]string{"name": "日本"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create folder status = %d body=%s", rec.Code, rec.Body.String())
	}
	var folder db.Folder
	if err := json.Unmarshal(rec.Body.Bytes(), &folder); err != nil {
		t.Fatal(err)
	}

	rec = post(fmt.Sprintf("/api/nodes/%d/ops", node.ID), admin, map[string]any{
		"group_id": folder.ID, "remark": "月付 5 刀", "expires_at": 1893456000, "monthly_cost_cents": 3500,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update ops status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Admin list must expose the ops metadata.
	req := newTestRequest("GET", "/api/nodes", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin list status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"monthly_cost_cents", "月付 5 刀", "group_name"} {
		if !strings.Contains(body, want) {
			t.Fatalf("admin list missing %q", want)
		}
	}

	// Tenant responses must never carry the admin-only fields.
	uid, cookie := loginAsUser(t, d, 10)
	if err := db.GrantNode(d, uid, node.ID, 5, 0); err != nil {
		t.Fatal(err)
	}
	req = newTestRequest("GET", "/api/my/rules", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant rules status = %d body=%s", rec.Code, rec.Body.String())
	}
	tenantBody := rec.Body.String()
	for _, leak := range []string{"monthly_cost_cents", "月付 5 刀", "\"remark\""} {
		if strings.Contains(tenantBody, leak) {
			t.Fatalf("tenant payload leaked admin-only field %q", leak)
		}
	}
}
