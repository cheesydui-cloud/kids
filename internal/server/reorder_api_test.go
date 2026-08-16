package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nft/internal/db"
)

func TestAPIReorderUsers(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	hash, _ := HashPassword("pw")
	var ids []int64
	for _, name := range []string{"ord-a", "ord-b", "ord-c"} {
		id, err := db.CreateUser(d, name, hash, "user")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	rec := adminPost(t, s, admin, "/api/users/reorder", map[string]any{
		"ids": []int64{ids[2], ids[0], ids[1]},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder users: %d %s", rec.Code, rec.Body.String())
	}

	users, err := db.ListUsers(d)
	if err != nil {
		t.Fatal(err)
	}
	got := pickUsernames(users, ids)
	if got != "ord-c,ord-a,ord-b" {
		t.Fatalf("order=%s want ord-c,ord-a,ord-b", got)
	}
}

func TestAPIReorderNodeRepo(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	var ids []int64
	for _, name := range []string{"repo-a", "repo-b", "repo-c"} {
		n, err := db.CreateNodeRepoEntry(d, name, "ss", "1.2.3.4", 443, "", "", 0, "", db.NodeRepoCFFields{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}

	rec := adminPost(t, s, admin, "/api/node-repo/reorder", map[string]any{
		"ids": []int64{ids[2], ids[0], ids[1]},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder repo: %d %s", rec.Code, rec.Body.String())
	}

	list, err := db.ListNodeRepo(d)
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for i, n := range list {
		if i > 0 {
			got += ","
		}
		got += n.Name
	}
	if got != "repo-c,repo-a,repo-b" {
		t.Fatalf("order=%s want repo-c,repo-a,repo-b", got)
	}

	req := newTestRequest("GET", "/api/node-repo", nil)
	req.AddCookie(admin)
	listRec := httptest.NewRecorder()
	s.Router().ServeHTTP(listRec, req)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", listRec.Code, listRec.Body.String())
	}
	var body struct {
		Nodes []db.NodeRepoEntry `json:"nodes"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Nodes) != 3 || body.Nodes[0].Name != "repo-c" {
		t.Fatalf("list after reorder = %+v", body.Nodes)
	}
}

func TestAPIChangeBackendIPUpdatesSiblings(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	ss, err := db.CreateNodeRepoEntry(d, "pv1", "ss", "ss.example.com", 36615, "", "", 0, "", db.NodeRepoCFFields{BackendIP: "70.231.54.201"})
	if err != nil {
		t.Fatal(err)
	}
	vl, err := db.CreateNodeRepoEntry(d, "pv1", "vless", "vl.example.com", 47632, "", "", 0, "", db.NodeRepoCFFields{BackendIP: "70.231.54.201"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateNodeRepoEntry(d, "other", "ss", "ot.example.com", 1, "", "", 0, "", db.NodeRepoCFFields{BackendIP: "1.2.3.4"})
	if err != nil {
		t.Fatal(err)
	}

	rec := adminPost(t, s, admin, "/api/node-repo/"+itoa(ss.ID)+"/backend-ip", map[string]any{
		"backend_ip": "8.8.8.8",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change ip: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Updated != 2 {
		t.Fatalf("updated=%d want 2", resp.Updated)
	}
	ss2, _ := db.GetNodeRepoEntry(d, ss.ID)
	vl2, _ := db.GetNodeRepoEntry(d, vl.ID)
	ot2, _ := db.GetNodeRepoEntry(d, other.ID)
	if ss2.BackendIP != "8.8.8.8" || vl2.BackendIP != "8.8.8.8" {
		t.Fatalf("siblings ip ss=%s vl=%s", ss2.BackendIP, vl2.BackendIP)
	}
	if ot2.BackendIP != "1.2.3.4" {
		t.Fatalf("unrelated changed to %s", ot2.BackendIP)
	}
}

func pickUsernames(users []*db.User, ids []int64) string {
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := ""
	for _, u := range users {
		if !want[u.ID] {
			continue
		}
		if out != "" {
			out += ","
		}
		out += u.Username
	}
	return out
}
