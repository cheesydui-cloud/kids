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

func TestBatchDeleteNodeRepoEntries(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	a, err := db.CreateNodeRepoEntry(d, "repo-a", "ss", "1.1.1.1", 8388, "", "", 0, "", db.NodeRepoCFFields{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateNodeRepoEntry(d, "repo-b", "ss", "2.2.2.2", 8388, "", "", 0, "", db.NodeRepoCFFields{})
	if err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"ids": []int64{a.ID, b.ID}})
	req := newTestRequest("POST", "/api/node-repo/batch-delete", bytes.NewReader(body))
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch delete status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Count int64 `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Count != 2 {
		t.Fatalf("deleted count = %d, want 2", resp.Count)
	}

	list, err := db.ListNodeRepo(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("node repo still has %d rows", len(list))
	}
}

func TestAuditLogEndpointFilters(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	uid, err := db.CreateUser(d, "audit-actor", "x", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WriteAudit(d, uid, "user.set_quota_bytes", "7", "10GB"); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteAudit(d, uid, "node.create", "node-1", "edge"); err != nil {
		t.Fatal(err)
	}

	get := func(target string) (int, map[string]any) {
		req := newTestRequest("GET", target, nil)
		req.AddCookie(admin)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		var payload map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		return rec.Code, payload
	}

	code, payload := get("/api/audit-logs")
	if code != http.StatusOK {
		t.Fatalf("GET /api/audit-logs status = %d", code)
	}
	if total, _ := payload["total"].(float64); total != 2 {
		t.Fatalf("unfiltered total = %v, want 2", payload["total"])
	}

	code, payload = get("/api/audit-logs?q=node.create")
	if code != http.StatusOK {
		t.Fatalf("filtered status = %d", code)
	}
	if total, _ := payload["total"].(float64); total != 1 {
		t.Fatalf("filtered total = %v, want 1", payload["total"])
	}
	logs, _ := payload["logs"].([]any)
	if len(logs) != 1 {
		t.Fatalf("filtered logs = %v, want one row", payload["logs"])
	}
	if first, _ := logs[0].(map[string]any); first["action"] != "node.create" {
		t.Fatalf("wrong action returned: %v", logs[0])
	}

	// Non-admins must not read the audit log.
	_, cookie := loginAsUser(t, d, 10)
	req := newTestRequest("GET", "/api/audit-logs", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin audit status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAnnouncementReadReceiptsAPI(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	uid, cookie := loginAsUser(t, d, 10)
	a, err := db.CreateAnnouncement(d, "标题", "内容", uid, 0, "", 0, "blue", 0)
	if err != nil {
		t.Fatal(err)
	}

	readIDs := func() []any {
		req := newTestRequest("GET", "/api/my/announcements", nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/my/announcements status = %d body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		ids, _ := payload["read_ids"].([]any)
		return ids
	}

	if ids := readIDs(); len(ids) != 0 {
		t.Fatalf("initial read_ids = %v, want empty", ids)
	}

	req := newTestRequest("POST", fmt.Sprintf("/api/my/announcements/%d/read", a.ID), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mark read status = %d body=%s", rec.Code, rec.Body.String())
	}

	ids := readIDs()
	if len(ids) != 1 {
		t.Fatalf("read_ids after mark = %v, want one entry", ids)
	}
	if got, _ := ids[0].(float64); int64(got) != a.ID {
		t.Fatalf("read_ids = %v, want %d", ids, a.ID)
	}

	// A notice the user cannot see must not be markable.
	other, err := db.CreateAnnouncement(d, "别人的", "内容", uid+999, 0, "", 0, "blue", 0)
	if err != nil {
		t.Fatal(err)
	}
	req = newTestRequest("POST", fmt.Sprintf("/api/my/announcements/%d/read", other.ID), nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("marking an invisible notice status = %d body=%s", rec.Code, rec.Body.String())
	}
}
