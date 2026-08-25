package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"nft/internal/db"
	"nft/internal/landing"
)

// googleTCPTarget is the user-facing “到谷歌” latency: last-hop agent TCP
// connect to Google HTTPS. Hop probes stay TCP, never ICMP.
const googleTCPTarget = "www.google.com:443"

// subItem is one client-importable node in a user's outbound subscription.
type subItem struct {
	Kind      string `json:"kind"` // relay | direct
	Name      string `json:"name"`
	Protocol  string `json:"protocol,omitempty"`
	URI       string `json:"uri"`
	ClashOK   bool   `json:"clash_ok"`
	RuleID    int64  `json:"rule_id,omitempty"`
	RuleName  string `json:"rule_name,omitempty"`
	Landing   string `json:"landing_name,omitempty"`
	ExpiresAt int64  `json:"expires_at,omitempty"`
	Family    string `json:"family,omitempty"` // v4 | v6
	// Status is the entry-node liveness shown on the user node list:
	// online / offline / unknown for relay, direct for ROLE_DIRECT exits.
	Status string `json:"status,omitempty"`
	// BlockReason is why this entry will not pass traffic even if the
	// client still has the URI. Empty means the panel has nothing to
	// blame besides the path itself.
	BlockReason string `json:"block_reason,omitempty"`
	BlockText   string `json:"block_text,omitempty"`
}

type subSkipped struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

type userSubProfile struct {
	Items   []subItem
	Skipped []subSkipped
}

func (s *Server) collectUserSub(u *db.User) userSubProfile {
	var p userSubProfile
	if u == nil {
		return p
	}
	idx := s.landingIndexFromDB(u.ID)
	roles := s.nodeRoleBits()
	used := map[string]int{}
	online := map[int64]int{}
	acctBlock, acctText := accountConnectBlock(u)
	exitByHP := map[string]*db.LandingExit{}
	exits, _ := db.PresentLandingExitsForUser(s.DB, u.ID)
	for _, e := range exits {
		if e != nil {
			exitByHP[e.Host+":"+strconv.Itoa(e.Port)] = e
		}
	}
	grantByNode := map[int64]*db.UserNode{}
	if ns, gs, err := db.ListNodesForUser(s.DB, u.ID); err == nil {
		for i := range ns {
			if i < len(gs) {
				grantByNode[ns[i].ID] = gs[i]
			}
		}
	}

	rules, _ := db.ListRulesByUser(s.DB, u.ID)
	for _, rl := range rules {
		if rl.Disabled {
			p.Skipped = append(p.Skipped, subSkipped{
				Kind: "rule", Reason: "disabled", Detail: rl.Name,
			})
			continue
		}
		item := s.buildRuleListItem(rl, u.Username)
		item.classifyExit(idx, true)
		if item.ExitKind != "landing" || item.RelayURI == "" {
			reason := "custom"
			if item.ExitKind == "landing" {
				reason = "no_entry"
			}
			p.Skipped = append(p.Skipped, subSkipped{
				Kind: "rule", Reason: reason, Detail: firstNonEmpty(rl.Name, item.Exit),
			})
			continue
		}
		base := buildSubDisplayName(u.Username, rl.Name, item.LandingExpiresAt, "relay")
		name := uniquifyName(base, used)
		st := s.ruleEntryStatus(rl.ID, online)
		block, text := relayConnectBlock(u, rl, st, acctBlock, acctText, exitByHP, grantByNode)
		p.Items = append(p.Items, subItem{
			Kind:        "relay",
			Name:        name,
			Protocol:    item.LandingProtocol,
			URI:         mustRenameURI(item.RelayURI, name),
			ClashOK:     clashOK(item.RelayURI),
			RuleID:      rl.ID,
			RuleName:    rl.Name,
			Landing:     item.LandingName,
			ExpiresAt:   item.LandingExpiresAt,
			Family:      "v4",
			Status:      st,
			BlockReason: block,
			BlockText:   text,
		})
		if item.RelayURIV6 != "" {
			n6 := uniquifyName(name+"-v6", used)
			p.Items = append(p.Items, subItem{
				Kind:        "relay",
				Name:        n6,
				Protocol:    item.LandingProtocol,
				URI:         mustRenameURI(item.RelayURIV6, n6),
				ClashOK:     clashOK(item.RelayURIV6),
				RuleID:      rl.ID,
				RuleName:    rl.Name,
				Landing:     item.LandingName,
				ExpiresAt:   item.LandingExpiresAt,
				Family:      "v6",
				Status:      st,
				BlockReason: block,
				BlockText:   text,
			})
		}
	}

	for _, e := range exits {
		if e == nil || e.URI == "" {
			continue
		}
		key := e.Protocol + ":" + e.Host + ":" + strconv.Itoa(e.Port)
		if roles[key]&roleDirect == 0 {
			continue
		}
		uri := e.URI
		display := e.Name
		if e.NameOverride != "" {
			display = e.NameOverride
			if rewritten, err := landing.RewriteName(uri, e.NameOverride); err == nil {
				uri = rewritten
			}
		}
		base := buildSubDisplayName(u.Username, display, e.ExpiresAt, "direct")
		name := uniquifyName(base, used)
		block, text := directConnectBlock(u, e, acctBlock, acctText)
		p.Items = append(p.Items, subItem{
			Kind:        "direct",
			Name:        name,
			Protocol:    e.Protocol,
			URI:         mustRenameURI(uri, name),
			ClashOK:     clashOK(uri),
			Landing:     display,
			ExpiresAt:   e.ExpiresAt,
			Status:      "direct",
			BlockReason: block,
			BlockText:   text,
		})
	}
	return p
}

func accountConnectBlock(u *db.User) (reason, text string) {
	if u == nil {
		return "", ""
	}
	if u.Disabled {
		why := "请联系管理员"
		if u.DisableReason.Valid && strings.TrimSpace(u.DisableReason.String) != "" {
			why = u.DisableReason.String
		}
		return "account_disabled", "账号已被禁用：" + why
	}
	if u.ExpiresAt.Valid && u.ExpiresAt.Int64 > 0 && u.ExpiresAt.Int64 < time.Now().Unix() {
		return "account_expired", "账号已过期，请联系管理员续期"
	}
	if u.TrafficQuotaBytes > 0 && userBillableTraffic(u) >= u.TrafficQuotaBytes {
		return "quota", "账号流量已用完，请联系管理员"
	}
	return "", ""
}

func landingConnectBlock(u *db.User, e *db.LandingExit) (reason, text string) {
	if e == nil {
		return "", ""
	}
	name := e.Name
	if e.NameOverride != "" {
		name = e.NameOverride
	}
	if name == "" {
		name = e.Host
	}
	if e.ExpiresAt > 0 && e.ExpiresAt <= time.Now().Unix() {
		return "landing_expired", "落地「" + name + "」已到期，请联系管理员续期"
	}
	rate := 1.0
	if u != nil && u.BillingRate > 0 {
		rate = u.BillingRate
	}
	if e.QuotaBytes > 0 && int64(float64(e.UsedBytes)*rate+0.5) >= e.QuotaBytes {
		return "landing_quota", "落地「" + name + "」流量已用完"
	}
	return "", ""
}

func relayConnectBlock(u *db.User, rl *db.Rule, entryStatus, acctReason, acctText string, exits map[string]*db.LandingExit, grants map[int64]*db.UserNode) (reason, text string) {
	if acctReason != "" {
		return acctReason, acctText
	}
	if entryStatus == "offline" {
		return "node_offline", "入口节点离线或已禁用"
	}
	if g := grants[rl.NodeID]; g != nil && g.TrafficQuotaBytes > 0 && g.TrafficUsedBytes >= g.TrafficQuotaBytes {
		return "node_quota", "该线路流量已用完"
	}
	if e := exits[rl.ExitHost+":"+strconv.Itoa(rl.ExitPort)]; e != nil {
		if r, t := landingConnectBlock(u, e); r != "" {
			return r, t
		}
	}
	return "", ""
}

func directConnectBlock(u *db.User, e *db.LandingExit, acctReason, acctText string) (reason, text string) {
	if acctReason != "" {
		return acctReason, acctText
	}
	return landingConnectBlock(u, e)
}

func (s *Server) ruleEntryStatus(ruleID int64, cache map[int64]int) string {
	hops, err := db.ListRuleHops(s.DB, ruleID)
	if err != nil || len(hops) == 0 {
		return "unknown"
	}
	nid := hops[0].NodeID
	if st, ok := cache[nid]; ok {
		if st == 1 {
			return "online"
		}
		if st < 0 {
			return "unknown"
		}
		return "offline"
	}
	n, err := db.GetNode(s.DB, nid)
	if err != nil || n == nil {
		cache[nid] = -1
		return "unknown"
	}
	s.reconcileNodeOnline([]*db.Node{n})
	if n.Disabled || n.Online != 1 {
		cache[nid] = 0
		return "offline"
	}
	cache[nid] = 1
	return "online"
}

func (s *Server) nodeRoleBits() map[string]int {
	val, _ := db.GetSetting(s.DB, "node_roles")
	roles := map[string]int{}
	if val == "" {
		return roles
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(val), &raw); err != nil {
		return roles
	}
	for k, v := range raw {
		var n int
		if err := json.Unmarshal(v, &n); err == nil {
			if n &= roleMask; n != 0 {
				roles[k] = n
			}
			continue
		}
		var str string
		if err := json.Unmarshal(v, &str); err == nil {
			switch str {
			case "landing":
				roles[k] = roleLanding
			case "direct":
				roles[k] = roleDirect
			}
		}
	}
	return roles
}

func buildSubDisplayName(username, extra string, expiresAt int64, kind string) string {
	user := strings.TrimSpace(username)
	extra = strings.TrimSpace(extra)
	day := fmtSubExpiry(expiresAt)
	parts := make([]string, 0, 3)
	if user != "" {
		parts = append(parts, user)
	}
	if extra != "" {
		parts = append(parts, extra)
	} else if kind == "direct" {
		parts = append(parts, "直连")
	}
	if day != "" {
		parts = append(parts, day)
	}
	if len(parts) == 0 {
		if kind == "direct" {
			return "直连"
		}
		return "proxy"
	}
	return strings.Join(parts, "-")
}

func fmtSubExpiry(unix int64) string {
	if unix <= 0 {
		return ""
	}
	t := time.Unix(unix, 0).In(time.Local)
	return fmt.Sprintf("%d月%d日", int(t.Month()), t.Day())
}

func uniquifyName(base string, used map[string]int) string {
	if base == "" {
		base = "proxy"
	}
	if used[base] == 0 {
		used[base] = 1
		return base
	}
	n := used[base] + 1
	for {
		name := fmt.Sprintf("%s-%d", base, n)
		if used[name] == 0 {
			used[base] = n
			used[name] = 1
			return name
		}
		n++
	}
}

func mustRenameURI(uri, name string) string {
	if uri == "" || name == "" {
		return uri
	}
	if rewritten, err := landing.RewriteName(uri, name); err == nil && rewritten != "" {
		return rewritten
	}
	return uri
}

func clashOK(uri string) bool {
	_, ok := landing.ClashProxyYAML(uri, "x")
	return ok
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (s *Server) requireSubTokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			}
		}
		if token == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		u, t, err := db.GetUserBySubToken(s.DB, token)
		if err != nil || u == nil || t == nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		if u.Disabled {
			http.Error(w, "account disabled", http.StatusForbidden)
			return
		}
		_ = db.TouchSubTokenUsage(s.DB, u.ID)
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

func (s *Server) writeSubscriptionUserinfo(w http.ResponseWriter, u *db.User) {
	if u == nil {
		return
	}
	expire := int64(0)
	if u.ExpiresAt.Valid && u.ExpiresAt.Int64 > 0 {
		expire = u.ExpiresAt.Int64
	}
	w.Header().Set("Subscription-Userinfo",
		fmt.Sprintf("upload=0; download=%d; total=%d; expire=%d",
			userBillableTraffic(u), u.TrafficQuotaBytes, expire))
	w.Header().Set("Profile-Update-Interval", "24")
}

func (s *Server) apiPublicSub(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	p := s.collectUserSub(u)
	flag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("flag")))
	if flag == "clash" || flag == "meta" || flag == "mihomo" || strings.HasSuffix(r.URL.Path, ".yaml") {
		s.writeClashBody(w, u, p, r.URL.Query().Get("download") == "1")
		return
	}
	uris := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		uris = append(uris, it.URI)
	}
	s.writeSubscriptionUserinfo(w, u)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(landing.EncodeURISubscription(uris)))
}

func (s *Server) apiPublicClash(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	p := s.collectUserSub(u)
	s.writeClashBody(w, u, p, r.URL.Query().Get("download") == "1" || strings.Contains(r.URL.Path, "mihomo"))
}

func (s *Server) writeClashBody(w http.ResponseWriter, u *db.User, p userSubProfile, asDownload bool) {
	named := make([]landing.NamedURI, 0, len(p.Items))
	for _, it := range p.Items {
		named = append(named, landing.NamedURI{Name: it.Name, URI: it.URI})
	}
	body := landing.ClashProfile(named)
	s.writeSubscriptionUserinfo(w, u)
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if asDownload {
		name := "nft.yaml"
		if u != nil && u.Username != "" {
			name = u.Username + ".yaml"
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	_, _ = w.Write([]byte(body))
}

func (s *Server) subscribeURLs(r *http.Request, token string) (uriURL, clashURL, mihomoURL string) {
	base := strings.TrimRight(panelBaseURL(s.DB, r), "/")
	if base == "" {
		host := r.Host
		if host == "" {
			host = "127.0.0.1"
		}
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + host
	}
	q := url.QueryEscape(token)
	uriURL = base + "/api/v1/sub?token=" + q
	clashURL = base + "/api/v1/clash.yaml?token=" + q
	mihomoURL = base + "/api/v1/mihomo.yaml?token=" + q
	return
}

func (s *Server) apiMySubscribe(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	token, err := db.EnsureSubToken(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "无法生成订阅口令")
		return
	}
	p := s.collectUserSub(u)
	uriURL, clashURL, mihomoURL := s.subscribeURLs(r, token)
	var expires any
	if u.ExpiresAt.Valid && u.ExpiresAt.Int64 != 0 {
		expires = u.ExpiresAt.Int64
	}
	reason := ""
	if u.DisableReason.Valid {
		reason = u.DisableReason.String
	}
	jsonOK(w, map[string]any{
		"token":      token,
		"uri_url":    uriURL,
		"clash_url":  clashURL,
		"mihomo_url": mihomoURL,
		"items":      p.Items,
		"skipped":    p.Skipped,
		"rules":      s.userSubRules(u, p),
		"account": map[string]any{
			"username":                 u.Username,
			"disabled":                 u.Disabled,
			"disable_reason":           reason,
			"max_forwards":             u.MaxForwards,
			"traffic_quota_bytes":      u.TrafficQuotaBytes,
			"traffic_used_bytes":       u.TrafficUsedBytes,
			"total_traffic_used_bytes": u.TotalTrafficUsedBytes,
			"billing_rate":             u.BillingRate,
			"expires_at":               expires,
		},
	})
}

func (s *Server) userSubRules(u *db.User, p userSubProfile) []map[string]any {
	rules, _ := db.ListRulesByUser(s.DB, u.ID)
	if len(rules) == 0 {
		return []map[string]any{}
	}
	byID := map[int64]subItem{}
	for _, it := range p.Items {
		if it.Kind == "relay" && it.RuleID > 0 {
			if _, ok := byID[it.RuleID]; !ok {
				byID[it.RuleID] = it
			}
		}
	}
	out := make([]map[string]any, 0, len(rules))
	for _, rl := range rules {
		row := map[string]any{
			"id":       rl.ID,
			"name":     rl.Name,
			"disabled": rl.Disabled,
		}
		if it, ok := byID[rl.ID]; ok {
			row["status"] = it.Status
			row["block_reason"] = it.BlockReason
			row["block_text"] = it.BlockText
			row["landing"] = it.Landing
		} else {
			row["status"] = "skipped"
			if rl.Disabled {
				row["block_reason"] = "disabled"
				row["block_text"] = "已停用，入口不再转发"
			}
		}
		out = append(out, row)
	}
	return out
}

func (s *Server) apiMyRotateSubscribe(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	token, err := db.RotateSubToken(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "重置失败")
		return
	}
	uriURL, clashURL, mihomoURL := s.subscribeURLs(r, token)
	db.WriteAudit(s.DB, u.ID, "user.rotate_subscribe", "", "")
	jsonOK(w, map[string]any{
		"token":      token,
		"uri_url":    uriURL,
		"clash_url":  clashURL,
		"mihomo_url": mihomoURL,
	})
}

type subLatencyItem struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	RuleID    int64  `json:"rule_id,omitempty"`
	Family    string `json:"family,omitempty"`
	OK        bool   `json:"ok"`
	LatencyMS int    `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

type subLatSnap struct {
	at     time.Time
	target string
	items  []subLatencyItem
}

const subLatTTL = 45 * time.Second

// apiMySubscribeLatency measures last-hop TCP connect time to Google for
// each of the caller's subscription items. Direct landings have no agent,
// so they return an explicit skip rather than a panel-side SSRF dial.
// Results are cached ~45s unless refresh=1.
func (s *Server) apiMySubscribeLatency(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	if !force {
		s.subLatMu.Lock()
		snap, ok := s.subLatCache[u.ID]
		s.subLatMu.Unlock()
		if ok && time.Since(snap.at) < subLatTTL {
			jsonOK(w, map[string]any{
				"target": snap.target,
				"items":  snap.items,
				"cached": true,
				"age_ms": time.Since(snap.at).Milliseconds(),
				"ttl_ms": subLatTTL.Milliseconds(),
			})
			return
		}
	}
	p := s.collectUserSub(u)
	exitOf := map[int64]int64{}
	need := map[int64]struct{}{}
	for _, it := range p.Items {
		if it.Kind != "relay" || it.RuleID <= 0 {
			continue
		}
		if _, ok := exitOf[it.RuleID]; ok {
			continue
		}
		nid := s.ruleExitNodeID(it.RuleID)
		exitOf[it.RuleID] = nid
		if nid > 0 {
			need[nid] = struct{}{}
		}
	}

	probed := make(map[int64]probeResult, len(need))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for nid := range need {
		nid := nid
		wg.Add(1)
		go func() {
			defer wg.Done()
			pr := s.probeGoogleViaNode(nid)
			mu.Lock()
			probed[nid] = pr
			mu.Unlock()
		}()
	}
	wg.Wait()

	out := make([]subLatencyItem, 0, len(p.Items))
	for _, it := range p.Items {
		row := subLatencyItem{Kind: it.Kind, Name: it.Name, RuleID: it.RuleID, Family: it.Family}
		if it.Kind != "relay" {
			row.Error = "直连节点无法测到谷歌"
			out = append(out, row)
			continue
		}
		nid := exitOf[it.RuleID]
		if nid <= 0 {
			row.Error = "无法探测"
			out = append(out, row)
			continue
		}
		pr := probed[nid]
		row.OK = pr.OK
		row.LatencyMS = pr.Latency
		row.Error = pr.Error
		out = append(out, row)
	}
	s.subLatMu.Lock()
	if s.subLatCache == nil {
		s.subLatCache = map[int64]subLatSnap{}
	}
	s.subLatCache[u.ID] = subLatSnap{at: time.Now(), target: googleTCPTarget, items: out}
	s.subLatMu.Unlock()
	jsonOK(w, map[string]any{
		"target": googleTCPTarget,
		"items":  out,
		"cached": false,
		"ttl_ms": subLatTTL.Milliseconds(),
	})
}

func (s *Server) ruleExitNodeID(ruleID int64) int64 {
	hops, err := db.ListRuleHops(s.DB, ruleID)
	if err != nil || len(hops) == 0 {
		return 0
	}
	return hops[len(hops)-1].NodeID
}

func (s *Server) probeGoogleViaNode(nodeID int64) probeResult {
	n, err := db.GetNode(s.DB, nodeID)
	if err != nil || n == nil {
		return probeResult{Error: "节点不存在"}
	}
	if n.Disabled {
		return probeResult{Error: "节点已停用"}
	}
	probeID := nodeID
	if n.NodeType == "composite" {
		hops, herr := db.ListNodeHops(s.DB, nodeID)
		if herr != nil || len(hops) == 0 {
			return probeResult{Error: "组合节点无子节点"}
		}
		probeID = hops[len(hops)-1].HopNodeID
	}
	ack, perr := s.Hub.SendProbe(probeID, googleTCPTarget)
	if perr != nil {
		return probeResult{Error: perr.Error()}
	}
	if !ack.OK {
		msg := ack.Error
		if msg == "" {
			msg = "不通"
		}
		return probeResult{Error: msg}
	}
	return probeResult{OK: true, Latency: ack.Latency}
}
