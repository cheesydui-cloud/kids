package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nft/internal/db"
)

func TestNodeRepoPureIPStillWorks(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	body := map[string]any{
		"name": "pro-664", "protocol": "ss",
		"host": "68.252.208.113", "port": 4865,
		"uri": "", "remark": "", "expires_at": 0, "group_id": 0,
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pure IP: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	node, _ := resp["node"].(map[string]any)
	if node == nil {
		t.Fatalf("want node in response, got %v", resp)
	}
	if node["host"] != "68.252.208.113" {
		t.Fatalf("host=%v", node["host"])
	}
	if node["cf_sync"] == true {
		t.Fatal("cf_sync should be false by default")
	}

	// list still returns the row
	req = newTestRequest("GET", "/api/node-repo", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Nodes []db.NodeRepoEntry `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Nodes) != 1 || list.Nodes[0].Host != "68.252.208.113" {
		t.Fatalf("list=%+v", list.Nodes)
	}
}

func TestNodeRepoCFSyncRequiresDomainAndIP(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	// CF on + bare IP host, no record name → 400
	body := map[string]any{
		"name": "x", "protocol": "ss",
		"host": "1.2.3.4", "port": 443,
		"cf_sync": true, "backend_ip": "1.2.3.4",
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for IP+cf_sync without record name, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "记录名") {
		t.Fatalf("want 记录名 hint, got %s", rec.Body.String())
	}

	// CF on + domain but missing backend_ip → 400
	body = map[string]any{
		"name": "x", "protocol": "ss",
		"host": "home.example.com", "port": 443,
		"cf_sync": true, "backend_ip": "",
	}
	buf, _ = json.Marshal(body)
	req = newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for missing backend_ip, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestNodeRepoCFSyncWithoutTokenRecordsError(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	body := map[string]any{
		"name": "ddns-1", "protocol": "ss",
		"host": "home.example.com", "port": 443,
		"cf_sync": true, "backend_ip": "203.0.113.10",
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create should succeed even if CF fails: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Node   db.NodeRepoEntry `json:"node"`
		CFSync cfSyncResult     `json:"cf_sync"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.CFSync.Attempted || resp.CFSync.OK {
		t.Fatalf("cf_sync=%+v", resp.CFSync)
	}
	if !strings.Contains(resp.CFSync.Message, "Token") {
		t.Fatalf("message=%q", resp.CFSync.Message)
	}
	// Row persisted with error
	got, err := db.GetNodeRepoEntry(d, resp.Node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CFSync || got.BackendIP != "203.0.113.10" {
		t.Fatalf("got %+v", got)
	}
	if got.CFLastError == "" {
		t.Fatal("expected cf_last_error set")
	}
}

func TestSettingsCFTokenMaskAndClear(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	// save token
	body := map[string]any{
		"panel_url":    "http://127.0.0.1:7788",
		"cf_api_token": "abcd1234efgh5678",
		"cf_zone_name": "example.com",
		"cf_ttl":       1,
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/settings", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}

	// get must not leak full token
	req = newTestRequest("GET", "/api/settings", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var settings map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &settings)
	if settings["cf_token_configured"] != true {
		t.Fatalf("settings=%v", settings)
	}
	raw, _ := json.Marshal(settings)
	if strings.Contains(string(raw), "abcd1234efgh5678") {
		t.Fatal("full token leaked in GET /settings")
	}
	stored, _ := db.GetSetting(d, "cf_api_token")
	if stored != "abcd1234efgh5678" {
		t.Fatalf("stored=%q", stored)
	}

	// clear
	body = map[string]any{"panel_url": "http://127.0.0.1:7788", "cf_clear_token": true}
	buf, _ = json.Marshal(body)
	req = newTestRequest("POST", "/api/settings", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ = db.GetSetting(d, "cf_api_token")
	if stored != "" {
		t.Fatalf("token not cleared: %q", stored)
	}
}

func TestNodeRepoDomainWithoutCF(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	body := map[string]any{
		"name": "dom", "protocol": "ss",
		"host": "landing.example.net", "port": 8443,
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create domain: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Node   db.NodeRepoEntry `json:"node"`
		CFSync cfSyncResult     `json:"cf_sync"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Node.Host != "landing.example.net" || resp.Node.CFSync {
		t.Fatalf("node=%+v", resp.Node)
	}
	if resp.CFSync.Attempted {
		t.Fatalf("should skip CF: %+v", resp.CFSync)
	}
}

func TestNodeRepoCFSyncSuccessWithMockAPI(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-xxxx" {
			t.Errorf("auth=%q", auth)
		}
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/dns_records"):
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[]}`))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/dns_records"):
			posts++
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"r1","type":"A","name":"home.example.com","content":"203.0.113.50","ttl":1,"proxied":false}}`))
		case r.Method == "PUT":
			posts++
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"r1","type":"A","name":"home.example.com","content":"203.0.113.99","ttl":1,"proxied":false}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[]}`))
		}
	}))
	t.Cleanup(srv.Close)

	_ = db.SetSetting(d, "cf_api_token", "test-token-xxxx")
	_ = db.SetSetting(d, "cf_api_base", srv.URL)
	_ = db.SetSetting(d, "cf_ttl", "1")

	body := map[string]any{
		"name": "cf-ok", "protocol": "ss",
		"host": "home.example.com", "port": 443,
		"cf_sync": true, "backend_ip": "203.0.113.50",
		"cf_zone_id": "zone99",
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Node   db.NodeRepoEntry `json:"node"`
		CFSync cfSyncResult     `json:"cf_sync"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.CFSync.Attempted || !resp.CFSync.OK {
		t.Fatalf("cf_sync=%+v", resp.CFSync)
	}
	if posts < 1 {
		t.Fatal("expected CF create POST")
	}
	got, err := db.GetNodeRepoEntry(d, resp.Node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CFLastError != "" || got.CFLastIP != "203.0.113.50" || got.CFLastSyncAt == 0 {
		t.Fatalf("got %+v", got)
	}

	// IP-only update: domain host unchanged → endpoint_changed false, CF updates
	body["backend_ip"] = "203.0.113.99"
	buf, _ = json.Marshal(body)
	req = newTestRequest("PATCH", "/api/node-repo/"+itoa(resp.Node.ID), bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	var up struct {
		EndpointChanged bool             `json:"endpoint_changed"`
		Node            db.NodeRepoEntry `json:"node"`
		CFSync          cfSyncResult     `json:"cf_sync"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		t.Fatal(err)
	}
	if up.EndpointChanged {
		t.Fatal("backend_ip-only must not cascade endpoint")
	}
	if !up.CFSync.OK {
		t.Fatalf("cf patch: %+v", up.CFSync)
	}
	if up.Node.BackendIP != "203.0.113.99" {
		t.Fatalf("backend_ip=%q", up.Node.BackendIP)
	}
}

func TestNodeRepoChangeIPAndProbe(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/dns_records") {
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"r1","type":"A","name":"p1.example.com","content":"1.1.1.1","ttl":1,"proxied":false}]}`))
			return
		}
		if r.Method == "PUT" || r.Method == "POST" {
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"r1","type":"A","name":"p1.example.com","content":"2.2.2.2","ttl":1,"proxied":false}}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[]}`))
	}))
	t.Cleanup(srv.Close)
	_ = db.SetSetting(d, "cf_api_token", "tok")
	_ = db.SetSetting(d, "cf_api_base", srv.URL)

	// create domain entry
	body := map[string]any{
		"name": "p1", "protocol": "ss", "host": "p1.example.com", "port": 443,
		"cf_sync": true, "backend_ip": "1.1.1.1", "cf_zone_id": "z1",
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Node db.NodeRepoEntry `json:"node"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.Node.ID

	// change IP only
	buf, _ = json.Marshal(map[string]any{"backend_ip": "2.2.2.2"})
	req = newTestRequest("POST", "/api/node-repo/"+itoa(id)+"/backend-ip", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("backend-ip %d %s", rec.Code, rec.Body.String())
	}
	var ch struct {
		Node   db.NodeRepoEntry `json:"node"`
		CFSync cfSyncResult     `json:"cf_sync"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)
	if ch.Node.BackendIP != "2.2.2.2" || ch.Node.Host != "p1.example.com" {
		t.Fatalf("node=%+v", ch.Node)
	}
	if !ch.CFSync.OK {
		t.Fatalf("cf=%+v", ch.CFSync)
	}

	// resync
	req = newTestRequest("POST", "/api/node-repo/"+itoa(id)+"/cf-resync", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("resync %d %s", rec.Code, rec.Body.String())
	}

	// probe literal path for pure IP entry
	n2, err := db.CreateNodeRepoEntry(d, "iponly", "ss", "9.9.9.9", 80, "", "", 0, "", db.NodeRepoCFFields{})
	if err != nil {
		t.Fatal(err)
	}
	req = newTestRequest("GET", "/api/node-repo/"+itoa(n2.ID)+"/probe-dns", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("probe %d %s", rec.Code, rec.Body.String())
	}
	var probe map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &probe)
	if probe["status"] != "literal_ip" {
		t.Fatalf("probe=%v", probe)
	}

	req = newTestRequest("POST", "/api/node-repo/cf-lookup", bytes.NewReader([]byte(`{"host":"p1.example.com","cf_zone_id":"z1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("cf-lookup %d %s", rec.Code, rec.Body.String())
	}
	var looked map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &looked)
	if looked["ip"] != "1.1.1.1" {
		t.Fatalf("lookup=%v", looked)
	}
}

func TestNodeRepoCFLookupByIPFillsRecordName(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	var sawContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/dns_records") {
			sawContent = r.URL.Query().Get("content")
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"r1","type":"A","name":"node.example.com","content":"82.22.26.185","ttl":1,"proxied":false}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[]}`))
	}))
	t.Cleanup(srv.Close)
	_ = db.SetSetting(d, "cf_api_token", "tok")
	_ = db.SetSetting(d, "cf_api_base", srv.URL)

	req := newTestRequest("POST", "/api/node-repo/cf-lookup", bytes.NewReader([]byte(`{"backend_ip":"82.22.26.185","cf_zone_id":"z1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup %d %s", rec.Code, rec.Body.String())
	}
	var looked map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &looked)
	if looked["record"] != "node.example.com" || looked["ip"] != "82.22.26.185" {
		t.Fatalf("lookup=%v content=%q", looked, sawContent)
	}
}

func TestNodeRepoCFLookupRequiresIPOrDomain(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)
	req := newTestRequest("POST", "/api/node-repo/cf-lookup", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestNodeRepoCFSyncIPHostWithRecordName(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	var lastName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && strings.Contains(r.URL.Path, "/dns_records"):
			lastName = r.URL.Query().Get("name")
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"r1","type":"A","name":"node.example.com","content":"82.22.26.185","ttl":1,"proxied":false}]}`))
		case r.Method == "POST" || r.Method == "PUT":
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"r1","type":"A","name":"node.example.com","content":"82.22.26.185","ttl":1,"proxied":false}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	_ = db.SetSetting(d, "cf_api_token", "tok")
	_ = db.SetSetting(d, "cf_api_base", srv.URL)

	req := newTestRequest("POST", "/api/node-repo/cf-lookup", bytes.NewReader([]byte(`{"host":"82.22.26.185","cf_record_name":"node.example.com","cf_zone_id":"z1"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup: %d %s", rec.Code, rec.Body.String())
	}
	var looked map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &looked)
	if looked["ip"] != "82.22.26.185" || looked["record"] != "node.example.com" {
		t.Fatalf("lookup=%v lastName=%q", looked, lastName)
	}

	body := map[string]any{
		"name": "ss-ip", "protocol": "ss",
		"host": "82.22.26.185", "port": 38846,
		"cf_sync": true, "backend_ip": "82.22.26.185",
		"cf_record_name": "node.example.com", "cf_zone_id": "z1",
	}
	buf, _ := json.Marshal(body)
	req = newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Node   db.NodeRepoEntry `json:"node"`
		CFSync cfSyncResult     `json:"cf_sync"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Node.Host != "82.22.26.185" || resp.Node.CFRecordName != "node.example.com" {
		t.Fatalf("node=%+v", resp.Node)
	}
	if !resp.CFSync.Attempted || !resp.CFSync.OK {
		t.Fatalf("cf_sync=%+v", resp.CFSync)
	}
	if resp.CFSync.Record != "node.example.com" {
		t.Fatalf("synced record=%q", resp.CFSync.Record)
	}

	buf, _ = json.Marshal(map[string]any{"backend_ip": "203.0.113.9"})
	req = newTestRequest("POST", "/api/node-repo/"+itoa(resp.Node.ID)+"/backend-ip", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backend-ip: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRewriteRepoShareURIUsesRecordAndName(t *testing.T) {
	uri := "vless://87f44ed6-2fd8-4ab3-b75c-b68534f7f1ee@31.40.214.186:9639?encryption=none&security=none#old"
	got := rewriteRepoShareURI(uri, "hostdare", "31.40.214.186", 9639)
	if !strings.Contains(got, "@31.40.214.186:9639") {
		t.Fatalf("without record name should keep IP: %s", got)
	}
	got = rewriteRepoShareURI(uri, "hostdare", repoShareHost("31.40.214.186", "cheesydu.cnodelink.com"), 9639)
	if !strings.Contains(got, "@cheesydu.cnodelink.com:9639") {
		t.Fatalf("want domain host, got %s", got)
	}
	if !strings.Contains(got, "hostdare") {
		t.Fatalf("want name hostdare, got %s", got)
	}
}

func TestNodeRepoSaveRewritesShareURI(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	body := map[string]any{
		"name": "hostdare", "protocol": "vless",
		"host": "31.40.214.186", "port": 9639,
		"uri":            "vless://87f44ed6-2fd8-4ab3-b75c-b68534f7f1ee@31.40.214.186:9639?encryption=none&security=none#old",
		"cf_record_name": "cheesydu.cnodelink.com",
	}
	buf, _ := json.Marshal(body)
	req := newTestRequest("POST", "/api/node-repo", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Node db.NodeRepoEntry `json:"node"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Node.Host != "31.40.214.186" {
		t.Fatalf("forward host must stay IP, got %q", resp.Node.Host)
	}
	if !strings.Contains(resp.Node.URI, "@cheesydu.cnodelink.com:9639") {
		t.Fatalf("share uri host=%s", resp.Node.URI)
	}
	if !strings.Contains(resp.Node.URI, "hostdare") {
		t.Fatalf("share uri name=%s", resp.Node.URI)
	}

	req = newTestRequest("GET", "/api/node-repo", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list struct {
		Nodes []db.NodeRepoEntry `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Nodes) != 1 || !strings.Contains(list.Nodes[0].URI, "@cheesydu.cnodelink.com:9639") {
		t.Fatalf("list uri=%+v", list.Nodes)
	}
}

func TestSettingsCFRecordsCRUD(t *testing.T) {
	d := openDB(t)
	s := newServer(t, d)
	admin := loginAsAdmin(t, d)

	var deleted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/zones"):
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"z1","name":"cnodelink.com"}]}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/dns_records"):
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"r1","type":"A","name":"a.cnodelink.com","content":"1.2.3.4","ttl":1,"proxied":false}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/dns_records"):
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"r2","type":"A","name":"b.cnodelink.com","content":"5.6.7.8","ttl":1,"proxied":false}}`))
		case r.Method == http.MethodDelete:
			deleted = r.URL.Path
			_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"r1"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"message":"unexpected"}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	_ = db.SetSetting(d, "cf_api_token", "tok")
	_ = db.SetSetting(d, "cf_zone_name", "cnodelink.com")
	_ = db.SetSetting(d, "cf_api_base", srv.URL)

	req := newTestRequest("GET", "/api/settings/cf-records", nil)
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Records) != 1 || listed.Records[0]["name"] != "a.cnodelink.com" {
		t.Fatalf("listed=%v", listed.Records)
	}

	buf, _ := json.Marshal(map[string]any{"name": "b.cnodelink.com", "content": "5.6.7.8"})
	req = newTestRequest("POST", "/api/settings/cf-records", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = newTestRequest("DELETE", "/api/settings/cf-records/r1", nil)
	req.AddCookie(admin)
	rec = httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(deleted, "/dns_records/r1") {
		t.Fatalf("deleted path=%s", deleted)
	}
}
