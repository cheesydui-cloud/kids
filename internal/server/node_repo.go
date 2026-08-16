package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nft/internal/cloudflare"
	"nft/internal/db"
	"nft/internal/landing"
	"nft/internal/resolver"
)

// apiListNodeRepo returns all nodes in the repository (admin only).
// Each entry includes user_count: how many users currently hold that host:port
// as a present source=repo landing exit.
func (s *Server) apiListNodeRepo(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListNodeRepo(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []db.NodeRepoEntry{}
	}
	if counts, err := db.CountRepoExitUsers(s.DB); err == nil && len(counts) > 0 {
		for i := range list {
			key := list[i].Host + ":" + strconv.Itoa(list[i].Port)
			list[i].UserCount = counts[key]
		}
	}
	for i := range list {
		list[i].URI = rewriteRepoShareURI(list[i].URI, list[i].Name, repoShareHost(list[i].Host, list[i].CFRecordName), list[i].Port)
	}
	jsonOK(w, map[string]any{"nodes": list})
}

// apiReorderNodeRepo persists the manual landing-repo display order from a
// drag-and-drop reorder in the admin node-repo list.
func (s *Server) apiReorderNodeRepo(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := db.ReorderNodeRepo(s.DB, body.IDs); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db.WriteAudit(s.DB, u.ID, "node_repo.reorder", "", "")
	jsonOK(w, map[string]any{"ok": true})
}

// apiListNodeRepoUsers returns users who currently use a repo node endpoint
// (present source=repo landing exit at the same host:port).
func (s *Server) apiListNodeRepoUsers(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	entry, err := db.GetNodeRepoEntry(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "节点不存在")
		return
	}
	users, err := db.ListRepoExitUsers(s.DB, entry.Host, entry.Port)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"node":  entry,
		"users": users,
	})
}

type nodeRepoBody struct {
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	URI          string `json:"uri"`
	Remark       string `json:"remark"`
	ExpiresAt    int64  `json:"expires_at"`
	GroupName    string `json:"group_name"`
	GroupID      int64  `json:"group_id"`
	BackendIP    string `json:"backend_ip"`
	CFSync       bool   `json:"cf_sync"`
	CFZoneID     string `json:"cf_zone_id"`
	CFRecordName string `json:"cf_record_name"`
}

func (b nodeRepoBody) cfFields() db.NodeRepoCFFields {
	return db.NodeRepoCFFields{
		BackendIP:    strings.TrimSpace(b.BackendIP),
		CFSync:       b.CFSync,
		CFZoneID:     strings.TrimSpace(b.CFZoneID),
		CFRecordName: strings.TrimSpace(b.CFRecordName),
	}
}

// repoShareHost is the hostname written into a copied/stored share URI.
// Prefer an explicit CF record name when the forwarding target is still an IP.
func repoShareHost(host, recordName string) string {
	rec := strings.TrimSpace(recordName)
	host = strings.TrimSpace(host)
	if rec != "" && net.ParseIP(host) != nil {
		return rec
	}
	if host != "" && net.ParseIP(host) == nil {
		return host
	}
	if rec != "" {
		return rec
	}
	return host
}

func rewriteRepoShareURI(uri, name, host string, port int) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return uri
	}
	out := uri
	if host != "" && port > 0 {
		if next, err := landing.RewriteEndpoint(uri, host, port); err == nil && next != "" {
			out = next
		}
	}
	if strings.TrimSpace(name) != "" {
		if next, err := landing.RewriteName(out, name); err == nil && next != "" {
			out = next
		}
	}
	return out
}

func applyRepoShareURI(body *nodeRepoBody, cf db.NodeRepoCFFields) {
	if body == nil {
		return
	}
	body.URI = rewriteRepoShareURI(body.URI, body.Name, repoShareHost(body.Host, cf.CFRecordName), body.Port)
}

func validateNodeRepoHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errBad("host is required")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if !resolver.PlausibleHostname(host) {
		return errBad("地址非法：不是合法 IP 或域名")
	}
	return nil
}

type badReq string

func (e badReq) Error() string { return string(e) }

func errBad(msg string) error { return badReq(msg) }

func validateNodeRepoCF(host string, cf db.NodeRepoCFFields) error {
	if !cf.CFSync {
		// backend_ip optional when not syncing; if set must be IPv4
		if cf.BackendIP != "" && !cloudflare.IsIPv4(cf.BackendIP) {
			return errBad("当前 IP 必须是 IPv4 地址")
		}
		return nil
	}
	// Sync on: need a domain to write (record name, or the target if it is a
	// hostname) plus the IPv4 that goes into the A record. SOCKS5 / Naive
	// forms keep the forwarding target as an IP and put the domain in 记录名.
	if _, err := cfRecordHost(host, cf.CFRecordName); err != nil {
		return err
	}
	if !cloudflare.IsIPv4(cf.BackendIP) {
		return errBad("开启 CF 同步时，须填写当前 IPv4")
	}
	return nil
}

// cfRecordHost is the Cloudflare A-record name: an explicit 记录名 wins;
// otherwise the target host is used when it is a domain. An IP target
// without a record name cannot be synced.
func cfRecordHost(host, recordName string) (string, error) {
	rec := strings.TrimSpace(recordName)
	if rec != "" {
		if net.ParseIP(rec) != nil {
			return "", errBad("记录名须为域名，不能是 IP")
		}
		if !resolver.PlausibleHostname(rec) {
			return "", errBad("记录名不是合法域名")
		}
		return rec, nil
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", errBad("请先填写目标地址或记录名")
	}
	if net.ParseIP(host) != nil {
		return "", errBad("目标地址是 IP 时，请填写记录名（要同步的域名）")
	}
	if !resolver.PlausibleHostname(host) {
		return "", errBad("目标地址不是合法域名")
	}
	return host, nil
}

// apiCreateNodeRepoEntry creates a new node in the repository.
func (s *Server) apiCreateNodeRepoEntry(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var body nodeRepoBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Host = strings.TrimSpace(body.Host)
	if body.Name == "" || body.Host == "" || body.Port == 0 {
		jsonErr(w, http.StatusBadRequest, "name, host and port are required")
		return
	}
	if body.Port < 1 || body.Port > 65535 {
		jsonErr(w, http.StatusBadRequest, "端口非法")
		return
	}
	if err := validateNodeRepoHost(body.Host); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cf := body.cfFields()
	if err := validateNodeRepoCF(body.Host, cf); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	applyRepoShareURI(&body, cf)
	groupName := strings.TrimSpace(body.GroupName)
	if body.GroupID > 0 {
		if f, err := db.GetNodeRepoFolder(s.DB, body.GroupID); err == nil {
			groupName = f.Name
		}
	}
	n, err := db.CreateNodeRepoEntry(s.DB, body.Name, body.Protocol, body.Host, body.Port, body.URI, body.Remark, body.ExpiresAt, groupName, cf)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfResult := s.maybeSyncNodeRepoCF(r.Context(), u.ID, &n)
	jsonOK(w, map[string]any{"node": n, "cf_sync": cfResult})
}

// apiUpdateNodeRepoEntry updates an existing node in the repository.
// When host/port (or URI metadata) changes, every user landing exit that was
// imported from the repo at the previous endpoint — and every rule still dialing
// that endpoint — is rewritten so admin + user UIs and the data plane follow.
//
// CF IP-only changes (domain host unchanged) do NOT cascade rules: agent DNS
// refresh picks up the new A record without rewriting exit_host.
func (s *Server) apiUpdateNodeRepoEntry(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body nodeRepoBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Host = strings.TrimSpace(body.Host)
	if body.Name == "" || body.Host == "" || body.Port == 0 {
		jsonErr(w, http.StatusBadRequest, "name, host and port are required")
		return
	}
	if body.Port < 1 || body.Port > 65535 {
		jsonErr(w, http.StatusBadRequest, "端口非法")
		return
	}
	if err := validateNodeRepoHost(body.Host); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cf := body.cfFields()
	if err := validateNodeRepoCF(body.Host, cf); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	applyRepoShareURI(&body, cf)
	prev, err := db.GetNodeRepoEntry(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "节点不存在")
		return
	}
	groupName := strings.TrimSpace(body.GroupName)
	if body.GroupID > 0 {
		if f, err := db.GetNodeRepoFolder(s.DB, body.GroupID); err == nil {
			groupName = f.Name
		}
	} else if body.GroupID == 0 && body.GroupName == "" {
		groupName = ""
	}
	if err := db.UpdateNodeRepoEntry(s.DB, id, body.Name, body.Protocol, body.Host, body.Port, body.URI, body.Remark, body.ExpiresAt, groupName, cf); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Cascade: repo template → user landing exits (source=repo) + rules/hops.
	// Always run so name/uri/expires refresh even when host:port is unchanged.
	prop, err := db.PropagateRepoExitChange(s.DB,
		prev.Host, body.Host, prev.Port, body.Port,
		body.Name, body.Protocol, body.URI, body.ExpiresAt,
	)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "仓库已保存，但同步用户/规则失败: "+err.Error())
		return
	}
	if len(prop.NodeIDs) > 0 {
		s.redispatchNodes(prop.NodeIDs)
	}

	n, _ := db.GetNodeRepoEntry(s.DB, id)
	cfResult := s.maybeSyncNodeRepoCF(r.Context(), u.ID, &n)

	jsonOK(w, map[string]any{
		"ok":               true,
		"endpoint_changed": prop.EndpointChanged,
		"exits_updated":    prop.ExitsUpdated,
		"rules_updated":    prop.RulesUpdated,
		"node":             n,
		"cf_sync":          cfResult,
	})
}

// cfSyncResult is returned to the UI after create/update.
type cfSyncResult struct {
	Attempted bool   `json:"attempted"`
	OK        bool   `json:"ok"`
	Skipped   bool   `json:"skipped,omitempty"`
	Message   string `json:"message,omitempty"`
	IP        string `json:"ip,omitempty"`
	Record    string `json:"record,omitempty"`
}

type nodeRepoCFSession struct {
	cli        *cloudflare.Client
	zoneID     string
	recordName string
}

// cfClientAndZone builds a CF client and resolves Zone ID (explicit or system default).
func (s *Server) cfClientAndZone(ctx context.Context, zoneID string) (*cloudflare.Client, string, error) {
	token, _ := db.GetSetting(s.DB, "cf_api_token")
	if strings.TrimSpace(token) == "" {
		return nil, "", fmt.Errorf("未配置 Cloudflare API Token（请到系统设置填写）")
	}
	cli := &cloudflare.Client{Token: cloudflare.SanitizeAPIToken(token)}
	if base, _ := db.GetSetting(s.DB, "cf_api_base"); strings.TrimSpace(base) != "" {
		cli.BaseURL = strings.TrimSpace(base)
	}
	zid := strings.TrimSpace(zoneID)
	if zid != "" {
		return cli, zid, nil
	}
	zoneName, _ := db.GetSetting(s.DB, "cf_zone_name")
	zoneName = strings.TrimSpace(zoneName)
	if zoneName == "" {
		return nil, "", fmt.Errorf("未指定 Zone（条目 cf_zone_id 为空且系统未设默认 Zone）")
	}
	id, err := cli.ResolveZoneID(ctx, zoneName)
	if err != nil {
		return nil, "", err
	}
	return cli, id, nil
}

func firstIPv4(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if cloudflare.IsIPv4(v) {
			return v
		}
	}
	return ""
}

func pickARecordByContent(recs []cloudflare.DNSRecord, prefer string) cloudflare.DNSRecord {
	prefer = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(prefer)), ".")
	if prefer != "" {
		for _, rec := range recs {
			if strings.TrimSuffix(strings.ToLower(rec.Name), ".") == prefer {
				return rec
			}
		}
	}
	return recs[0]
}

func cfLookupNames(recs []cloudflare.DNSRecord) []string {
	out := make([]string, 0, len(recs))
	seen := map[string]bool{}
	for _, rec := range recs {
		name := strings.TrimSuffix(rec.Name, ".")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// nodeRepoCFSession builds a CF client and resolves zone + record name.
// persistZoneID writes the looked-up zone onto an existing repo row.
func (s *Server) nodeRepoCFSession(ctx context.Context, host, zoneID, recordName string, persist *db.NodeRepoEntry) (nodeRepoCFSession, error) {
	name, err := cfRecordHost(host, recordName)
	if err != nil {
		return nodeRepoCFSession{}, err
	}
	cli, zid, err := s.cfClientAndZone(ctx, zoneID)
	if err != nil {
		return nodeRepoCFSession{}, err
	}
	if persist != nil && persist.ID > 0 && strings.TrimSpace(persist.CFZoneID) == "" && zid != "" {
		_ = db.UpdateNodeRepoEntry(s.DB, persist.ID, persist.Name, persist.Protocol, persist.Host, persist.Port, persist.URI, persist.Remark, persist.ExpiresAt, persist.GroupName, db.NodeRepoCFFields{
			BackendIP: persist.BackendIP, CFSync: persist.CFSync, CFZoneID: zid, CFRecordName: persist.CFRecordName,
		})
		persist.CFZoneID = zid
	}
	return nodeRepoCFSession{
		cli:        cli,
		zoneID:     zid,
		recordName: cloudflare.RecordNameForHost(name, recordName),
	}, nil
}

// maybeSyncNodeRepoCF pushes an A record when the entry has cf_sync enabled.
// Failures are recorded on the row and returned; the repo save itself already
// succeeded so the admin can fix token/IP and retry.
func (s *Server) maybeSyncNodeRepoCF(ctx context.Context, adminID int64, n *db.NodeRepoEntry) cfSyncResult {
	if n == nil || !n.CFSync {
		return cfSyncResult{Attempted: false, Skipped: true, Message: "未开启 CF 同步"}
	}
	sess, err := s.nodeRepoCFSession(ctx, n.Host, n.CFZoneID, n.CFRecordName, n)
	if err != nil {
		msg := err.Error()
		_ = db.SetNodeRepoCFSyncResult(s.DB, n.ID, false, "", msg)
		n.CFLastError = msg
		if strings.Contains(msg, "Zone") || strings.Contains(msg, "Token") {
			return cfSyncResult{Attempted: true, OK: false, Message: msg}
		}
		db.WriteAudit(s.DB, adminID, "cf.dns.fail", n.Host, msg)
		return cfSyncResult{Attempted: true, OK: false, Message: msg}
	}
	zoneID := sess.zoneID
	recordName := sess.recordName
	ttlStr, _ := db.GetSetting(s.DB, "cf_ttl")
	ttl := 1
	if t, err := strconv.Atoi(strings.TrimSpace(ttlStr)); err == nil && t > 0 {
		ttl = t
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	rec, err := sess.cli.UpsertARecord(ctx, zoneID, recordName, n.BackendIP, ttl)
	if err != nil {
		msg := err.Error()
		_ = db.SetNodeRepoCFSyncResult(s.DB, n.ID, false, "", msg)
		n.CFLastError = msg
		db.WriteAudit(s.DB, adminID, "cf.dns.fail", recordName, msg)
		return cfSyncResult{Attempted: true, OK: false, Message: msg, Record: recordName, IP: n.BackendIP}
	}
	_ = db.SetNodeRepoCFSyncResult(s.DB, n.ID, true, n.BackendIP, "")
	n.CFLastError = ""
	n.CFLastIP = n.BackendIP
	n.CFLastSyncAt = time.Now().Unix()
	db.WriteAudit(s.DB, adminID, "cf.dns.update", recordName, n.BackendIP)
	return cfSyncResult{
		Attempted: true,
		OK:        true,
		Message:   "已同步 A 记录",
		IP:        rec.Content,
		Record:    recordName,
	}
}

// apiBatchSetNodeRepoGroup moves many repo entries into a folder.
// Prefer group_id; group_name creates/ensures a folder for legacy clients.
func (s *Server) apiBatchSetNodeRepoGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs       []int64 `json:"ids"`
		GroupID   *int64  `json:"group_id"`
		GroupName string  `json:"group_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.IDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "no nodes selected")
		return
	}
	if body.GroupID != nil {
		if err := db.SetNodeRepoFoldersBatch(s.DB, body.IDs, *body.GroupID); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if err := db.SetNodeRepoGroupsBatch(s.DB, body.IDs, strings.TrimSpace(body.GroupName)); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true, "count": len(body.IDs)})
}

// --- Node-repo folders ---

func (s *Server) apiListNodeRepoFolders(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListNodeRepoFolders(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*db.Folder{}
	}
	var ungrouped int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM node_repo WHERE group_id=0`).Scan(&ungrouped)
	jsonOK(w, map[string]any{"folders": list, "ungrouped": ungrouped})
}

func (s *Server) apiCreateNodeRepoFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	f, err := db.CreateNodeRepoFolder(s.DB, body.Name)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, f)
}

func (s *Server) apiRenameNodeRepoFolder(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := db.RenameNodeRepoFolder(s.DB, id, body.Name); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) apiDeleteNodeRepoFolder(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := db.DeleteNodeRepoFolder(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// apiDeleteNodeRepoEntry deletes a node from the repository.
func (s *Server) apiDeleteNodeRepoEntry(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := db.DeleteNodeRepoEntry(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

// apiAssignRepoToUser takes selected repo node IDs and assigns them to a user
// as landing exits. Uses Append (not Sync) so existing exits are preserved.
// expires_at from the repo entry is carried over to the user's landing exit.
func (s *Server) apiAssignRepoToUser(w http.ResponseWriter, r *http.Request) {
	uidStr := r.PathValue("id")
	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var body struct {
		NodeIDs []int64 `json:"node_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.NodeIDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "no nodes selected")
		return
	}
	entries, err := db.ListNodeRepoByIDs(s.DB, body.NodeIDs)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var inputs []db.LandingExitInput
	for _, e := range entries {
		inputs = append(inputs, db.LandingExitInput{
			Host:     e.Host,
			Port:     e.Port,
			Name:     e.Name,
			Protocol: e.Protocol,
			URI:      e.URI,
		})
	}
	flipped, err := db.AppendUserLandingExits(s.DB, uid, inputs)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, k := range flipped {
		s.goAsync(func() { s.redispatchUserExit(uid, k.Host, k.Port) })
	}
	for _, e := range entries {
		if e.ExpiresAt > 0 {
			_, _, _ = db.SetUserLandingExitExpires(s.DB, uid, e.Host, e.Port, e.ExpiresAt)
		}
	}
	exits, _ := db.ListUserLandingExits(s.DB, uid)
	jsonOK(w, map[string]any{"ok": true, "assigned": len(inputs), "exits": exits})
}

// apiSetNodeRepoBackendIP updates only the current IPv4 (and optionally forces
// CF sync). Domain host/port stay unchanged so rules are not cascaded.
func (s *Server) apiSetNodeRepoBackendIP(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		BackendIP string `json:"backend_ip"`
		CFSync    *bool  `json:"cf_sync"` // nil = leave; true/false = set
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ip := strings.TrimSpace(body.BackendIP)
	if !cloudflare.IsIPv4(ip) {
		jsonErr(w, http.StatusBadRequest, "当前 IP 必须是 IPv4 地址")
		return
	}
	n, err := db.GetNodeRepoEntry(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "节点不存在")
		return
	}
	cfSync := n.CFSync
	if body.CFSync != nil {
		cfSync = *body.CFSync
	}
	cf := db.NodeRepoCFFields{
		BackendIP: ip, CFSync: cfSync,
		CFZoneID: n.CFZoneID, CFRecordName: n.CFRecordName,
	}
	if err := validateNodeRepoCF(n.Host, cf); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.UpdateNodeRepoEntry(s.DB, id, n.Name, n.Protocol, n.Host, n.Port, n.URI, n.Remark, n.ExpiresAt, n.GroupName, cf); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, _ = db.GetNodeRepoEntry(s.DB, id)
	cfResult := s.maybeSyncNodeRepoCF(r.Context(), u.ID, &n)
	jsonOK(w, map[string]any{"ok": true, "node": n, "cf_sync": cfResult})
}

// apiResyncNodeRepoCF re-pushes the A record without changing other fields.
func (s *Server) apiResyncNodeRepoCF(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	n, err := db.GetNodeRepoEntry(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "节点不存在")
		return
	}
	if !n.CFSync {
		jsonErr(w, http.StatusBadRequest, "未开启 CF 同步")
		return
	}
	if !cloudflare.IsIPv4(n.BackendIP) {
		jsonErr(w, http.StatusBadRequest, "当前 IP 无效，请先设置 IPv4")
		return
	}
	cfResult := s.maybeSyncNodeRepoCF(r.Context(), u.ID, &n)
	n, _ = db.GetNodeRepoEntry(s.DB, id)
	jsonOK(w, map[string]any{"ok": true, "node": n, "cf_sync": cfResult})
}

// apiLookupNodeRepoCF reads Cloudflare DNS for the add/edit form.
// With an IPv4 (当前 IP / host) and no record name, it finds the matching A
// record and returns the domain so the UI can fill 「记录名」. With a record
// name it still returns that A record's IPv4.
func (s *Server) apiLookupNodeRepoCF(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host         string `json:"host"`
		IP           string `json:"ip"`
		BackendIP    string `json:"backend_ip"`
		CFZoneID     string `json:"cf_zone_id"`
		CFRecordName string `json:"cf_record_name"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonErr(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	host := strings.TrimSpace(body.Host)
	recordName := strings.TrimSpace(body.CFRecordName)
	ip := firstIPv4(body.BackendIP, body.IP, host)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if ip != "" && recordName == "" {
		cli, zid, err := s.cfClientAndZone(ctx, strings.TrimSpace(body.CFZoneID))
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		recs, err := cli.FindARecordsByContent(ctx, zid, ip)
		if err != nil {
			jsonErr(w, http.StatusBadGateway, err.Error())
			return
		}
		if len(recs) == 0 {
			jsonErr(w, http.StatusNotFound, "Cloudflare 上没有指向 "+ip+" 的 A 记录")
			return
		}
		chosen := pickARecordByContent(recs, host)
		names := cfLookupNames(recs)
		msg := "已根据 IP 同步记录名 " + chosen.Name
		if len(names) > 1 {
			msg += "（同 IP 共 " + strconv.Itoa(len(names)) + " 条）"
		}
		jsonOK(w, map[string]any{
			"ok":      true,
			"ip":      ip,
			"record":  chosen.Name,
			"records": names,
			"zone_id": zid,
			"proxied": chosen.Proxied,
			"ttl":     chosen.TTL,
			"message": msg,
		})
		return
	}

	name, err := cfRecordHost(host, recordName)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sess, err := s.nodeRepoCFSession(ctx, name, strings.TrimSpace(body.CFZoneID), recordName, nil)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, err := sess.cli.GetARecord(ctx, sess.zoneID, sess.recordName)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if rec == nil || !cloudflare.IsIPv4(rec.Content) {
		jsonErr(w, http.StatusNotFound, "Cloudflare 上没有这条 A 记录："+sess.recordName)
		return
	}
	jsonOK(w, map[string]any{
		"ok":      true,
		"ip":      rec.Content,
		"record":  sess.recordName,
		"zone_id": sess.zoneID,
		"proxied": rec.Proxied,
		"ttl":     rec.TTL,
		"message": "已从 Cloudflare 拉取 " + rec.Content,
	})
}

// apiProbeNodeRepoDNS resolves the entry host and compares to backend_ip.
func (s *Server) apiProbeNodeRepoDNS(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	n, err := db.GetNodeRepoEntry(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "节点不存在")
		return
	}
	host := strings.TrimSpace(n.Host)
	out := map[string]any{
		"host":       host,
		"backend_ip": n.BackendIP,
		"is_domain":  net.ParseIP(host) == nil && resolver.PlausibleHostname(host),
	}
	if net.ParseIP(host) != nil {
		out["status"] = "literal_ip"
		out["message"] = "目标地址是 IP，无需 DNS 解析"
		out["resolved"] = []string{}
		out["match"] = host == n.BackendIP || n.BackendIP == ""
		jsonOK(w, out)
		return
	}
	if !resolver.PlausibleHostname(host) {
		jsonErr(w, http.StatusBadRequest, "目标地址不是合法域名")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res := resolver.New()
	ip, err := res.LookupIPv4(ctx, host)
	if err != nil {
		out["status"] = "fail"
		out["message"] = "解析失败: " + err.Error()
		out["resolved"] = []string{}
		out["match"] = false
		jsonOK(w, out)
		return
	}
	out["resolved"] = []string{ip}
	match := n.BackendIP != "" && ip == n.BackendIP
	out["match"] = match
	if n.BackendIP == "" {
		out["status"] = "ok"
		out["message"] = "解析到 " + ip + "（未设置当前 IP，无法对比）"
	} else if match {
		out["status"] = "match"
		out["message"] = "解析与当前 IP 一致"
	} else {
		out["status"] = "mismatch"
		out["message"] = "解析到 " + ip + "，与当前 IP " + n.BackendIP + " 不一致（DNS 可能未生效）"
	}
	jsonOK(w, out)
}
