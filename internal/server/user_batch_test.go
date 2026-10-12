package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nft/internal/db"
)

func TestCreateUserDuplicateNameAndResetDays(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	post := func(username string, days int) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"username":           username,
			"password":           "pw",
			"role":               "user",
			"traffic_reset_days": days,
		})
		req := newTestRequest("POST", "/api/users", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(admin)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		return rec
	}

	rec := post("dup-user", 30)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		User db.User `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.User.TrafficResetDays != 30 {
		t.Fatalf("traffic_reset_days = %d, want 30", created.User.TrafficResetDays)
	}

	rec = post("dup-user", 0)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate: %d %s", rec.Code, rec.Body.String())
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody.Error != "用户名已存在" {
		t.Fatalf("error = %q, want 用户名已存在", errBody.Error)
	}
}

func TestListUsersIncludesOpenRequests(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	hash, _ := HashPassword("pw")
	uid, err := db.CreateUser(d, "req-user", hash, "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CreateUserRequest(d, uid, db.RequestRenew, "please"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.CreateUserRequest(d, uid, db.RequestQuota, ""); err != nil {
		t.Fatal(err)
	}

	req := newTestRequest("GET", "/api/users", nil)
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Users []db.User `json:"users"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var found *db.User
	for i := range resp.Users {
		if resp.Users[i].ID == uid {
			found = &resp.Users[i]
			break
		}
	}
	if found == nil {
		t.Fatal("user missing from list")
	}
	if len(found.OpenRequests) != 2 || found.OpenRequests[0] != "renew" || found.OpenRequests[1] != "quota" {
		t.Fatalf("open_requests = %#v", found.OpenRequests)
	}
}

func TestBatchRenewDisableAndQuota(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	hash, _ := HashPassword("pw")
	uid, err := db.CreateUser(d, "batch-user", hash, "user")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(48 * time.Hour).Unix()
	if _, err := d.Exec(`UPDATE users SET expires_at=? WHERE id=?`, start, uid); err != nil {
		t.Fatal(err)
	}

	call := func(payload map[string]any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(payload)
		req := newTestRequest("POST", "/api/users/batch", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(admin)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("batch %v: %d %s", payload["action"], rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	out := call(map[string]any{"ids": []int64{uid}, "action": "renew", "days": 10})
	if out["updated"] != float64(1) {
		t.Fatalf("renew updated = %v", out["updated"])
	}
	u, err := db.GetUserByID(d, uid)
	if err != nil {
		t.Fatal(err)
	}
	want := db.ExtendExpiry(start, time.Now().Unix(), 10)
	if !u.ExpiresAt.Valid || u.ExpiresAt.Int64 != want {
		t.Fatalf("expires_at = %d, want %d", u.ExpiresAt.Int64, want)
	}

	call(map[string]any{"ids": []int64{uid}, "action": "disable"})
	u, _ = db.GetUserByID(d, uid)
	if !u.Disabled || !u.DisableReason.Valid || u.DisableReason.String != "管理员手动禁用" {
		t.Fatalf("disable state = %+v", u.DisableReason)
	}

	call(map[string]any{"ids": []int64{uid}, "action": "quota", "traffic_quota_gb": 2})
	u, _ = db.GetUserByID(d, uid)
	if u.TrafficQuotaBytes != 2*1073741824 {
		t.Fatalf("quota = %d", u.TrafficQuotaBytes)
	}

	call(map[string]any{"ids": []int64{uid}, "action": "rate", "billing_rate": 1.5})
	u, _ = db.GetUserByID(d, uid)
	if u.BillingRate != 1.5 {
		t.Fatalf("rate = %v", u.BillingRate)
	}

	if _, err := d.Exec(`UPDATE users SET traffic_used_bytes=100 WHERE id=?`, uid); err != nil {
		t.Fatal(err)
	}
	call(map[string]any{"ids": []int64{uid}, "action": "reset_traffic"})
	u, _ = db.GetUserByID(d, uid)
	if u.TrafficUsedBytes != 0 {
		t.Fatalf("used after reset = %d", u.TrafficUsedBytes)
	}
}
