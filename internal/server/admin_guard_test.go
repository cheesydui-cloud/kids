package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"nft/internal/db"
)

// Admin accounts can't be reset or deleted through the user-management API,
// regardless of any frontend gating.
func TestAdminUserCannotBeResetOrDeleted(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	hash, _ := HashPassword("pw")
	otherAdmin, err := db.CreateUser(d, "admin-2", hash, "admin")
	if err != nil {
		t.Fatal(err)
	}
	regular, err := db.CreateUser(d, "user-1", hash, "user")
	if err != nil {
		t.Fatal(err)
	}

	do := func(method, path string) int {
		req := newTestRequest(method, path, nil)
		req.AddCookie(admin)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do("POST", fmt.Sprintf("/api/users/%d/reset-password", otherAdmin)); code != http.StatusForbidden {
		t.Errorf("reset-password admin: status = %d, want 403", code)
	}
	if code := do("DELETE", fmt.Sprintf("/api/users/%d", otherAdmin)); code != http.StatusForbidden {
		t.Errorf("delete admin: status = %d, want 403", code)
	}
	// A regular user is still resettable/deletable.
	if code := do("POST", fmt.Sprintf("/api/users/%d/reset-password", regular)); code != http.StatusOK {
		t.Errorf("reset-password regular: status = %d, want 200", code)
	}
	if code := do("DELETE", fmt.Sprintf("/api/users/%d", regular)); code != http.StatusOK {
		t.Errorf("delete regular: status = %d, want 200", code)
	}
}

func TestAdminOnlyDashboardSubFetchNodeRoles(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	_, userCookie := loginAsUser(t, d, 10)
	admin := loginAsAdmin(t, d)

	assert := func(method, path string, wantUser, wantAdmin int) {
		t.Helper()
		req := newTestRequest(method, path, nil)
		req.AddCookie(userCookie)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		if rec.Code != wantUser {
			t.Errorf("%s %s as user: %d, want %d", method, path, rec.Code, wantUser)
		}
		req = newTestRequest(method, path, nil)
		req.AddCookie(admin)
		rec = httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		if rec.Code != wantAdmin {
			t.Errorf("%s %s as admin: %d, want %d body=%s", method, path, rec.Code, wantAdmin, rec.Body.String())
		}
	}
	assert("GET", "/api/dashboard", http.StatusForbidden, http.StatusOK)
	assert("GET", "/api/node-roles", http.StatusForbidden, http.StatusOK)
	assert("GET", "/api/settings/update?status=1", http.StatusForbidden, http.StatusOK)

	req := newTestRequest("POST", "/api/sub-fetch", bytes.NewReader([]byte(`{"url":"https://example.com/sub"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(userCookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/sub-fetch as user: %d, want 403", rec.Code)
	}
}

func TestCreateUserPersistsBillingRateAndShanghaiExpiry(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	body, _ := json.Marshal(map[string]any{
		"username":            "rate-user",
		"password":            "pw",
		"role":                "user",
		"billing_rate":        2.5,
		"expires_at":          "2026-08-15",
		"traffic_quota_bytes": 1073741824,
	})
	req := newTestRequest("POST", "/api/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		User db.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.User.BillingRate != 2.5 {
		t.Fatalf("billing_rate = %v, want 2.5", resp.User.BillingRate)
	}
	want, err := db.ParseBusinessDateEnd("2026-08-15")
	if err != nil {
		t.Fatal(err)
	}
	if !resp.User.ExpiresAt.Valid || resp.User.ExpiresAt.Int64 != want {
		t.Fatalf("expires_at = %+v, want %d", resp.User.ExpiresAt, want)
	}
}

func TestCreateUserAssignsGroup(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	folder, err := db.CreateUserFolder(d, "VIP")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"username": "grouped-user",
		"password": "pw",
		"role":     "user",
		"group_id": folder.ID,
	})
	req := newTestRequest("POST", "/api/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		User db.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.User.GroupID != folder.ID || resp.User.GroupName != "VIP" {
		t.Fatalf("group = %d/%q, want %d/VIP", resp.User.GroupID, resp.User.GroupName, folder.ID)
	}
}

func TestSelfNodeMutationsRejected(t *testing.T) {
	d := openDB(t)
	self, err := db.UpsertSelfNode(d)
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	uid, _ := loginAsUser(t, d, 10)

	assert400 := func(method, path string) {
		t.Helper()
		req := newTestRequest(method, path, nil)
		req.AddCookie(admin)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s: %d, want 400 body=%s", method, path, rec.Code, rec.Body.String())
		}
	}
	id := fmt.Sprintf("%d", self.ID)
	assert400("DELETE", "/api/nodes/"+id)
	assert400("POST", "/api/nodes/"+id+"/toggle")
	assert400("POST", "/api/nodes/"+id+"/reset-token")
	assert400("POST", "/api/nodes/"+id+"/upgrade")

	body, _ := json.Marshal(map[string]any{"node_id": self.ID, "max_forwards": 5})
	req := newTestRequest("POST", fmt.Sprintf("/api/users/%d/grants", uid), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("grant self: %d, want 400 body=%s", rec.Code, rec.Body.String())
	}
}
