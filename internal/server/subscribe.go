package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nft/internal/db"
	"nft/internal/landing"
)

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
		p.Items = append(p.Items, subItem{
			Kind:      "relay",
			Name:      name,
			Protocol:  item.LandingProtocol,
			URI:       mustRenameURI(item.RelayURI, name),
			ClashOK:   clashOK(item.RelayURI),
			RuleID:    rl.ID,
			RuleName:  rl.Name,
			Landing:   item.LandingName,
			ExpiresAt: item.LandingExpiresAt,
			Family:    "v4",
			Status:    st,
		})
		if item.RelayURIV6 != "" {
			n6 := uniquifyName(name+"-v6", used)
			p.Items = append(p.Items, subItem{
				Kind:      "relay",
				Name:      n6,
				Protocol:  item.LandingProtocol,
				URI:       mustRenameURI(item.RelayURIV6, n6),
				ClashOK:   clashOK(item.RelayURIV6),
				RuleID:    rl.ID,
				RuleName:  rl.Name,
				Landing:   item.LandingName,
				ExpiresAt: item.LandingExpiresAt,
				Family:    "v6",
				Status:    st,
			})
		}
	}

	exits, _ := db.PresentLandingExitsForUser(s.DB, u.ID)
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
		p.Items = append(p.Items, subItem{
			Kind:      "direct",
			Name:      name,
			Protocol:  e.Protocol,
			URI:       mustRenameURI(uri, name),
			ClashOK:   clashOK(uri),
			Landing:   display,
			ExpiresAt: e.ExpiresAt,
			Status:    "direct",
		})
	}
	return p
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
	switch kind {
	case "direct":
		if user != "" && extra != "" {
			return user + "-直连-" + extra
		}
		if user != "" {
			return user + "-直连"
		}
		if extra != "" {
			return extra
		}
		return "直连"
	default:
		if user != "" && day != "" {
			return user + "-" + day
		}
		if user != "" && extra != "" {
			return user + "-" + extra
		}
		if user != "" {
			return user
		}
		if extra != "" {
			return extra
		}
		return "proxy"
	}
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
			u.TrafficUsedBytes, u.TrafficQuotaBytes, expire))
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
	showRate, _ := db.GetSetting(s.DB, "show_rate_to_user")
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
		"show_rate":  showRate == "1",
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

func (s *Server) apiMyRotateSubscribe(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	token, err := db.RotateSubToken(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "重置失败")
		return
	}
	uriURL, clashURL, mihomoURL := s.subscribeURLs(r, token)
	jsonOK(w, map[string]any{
		"token":      token,
		"uri_url":    uriURL,
		"clash_url":  clashURL,
		"mihomo_url": mihomoURL,
	})
}
