package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"nft/internal/db"
)

const (
	panelUpdateStatusName = "panel-update.json"
	panelUpdateLogName    = "panel-update.log"
	panelUpdateScriptName = "panel-self-update.sh"
	panelUpdateInstallSH  = "panel-self-update-install.sh"
	nftUpgradePath        = "/usr/local/sbin/nft-upgrade"
	ghProxyFile           = "/etc/nft/gh-proxy"
	panelUpdateStaleAfter = 20 * time.Minute
	latestReleaseCacheFor = 45 * time.Second
	maxReleaseNotes       = 4000
	maxUpdateLogTail      = 8 << 10
)

// panelUpdateDir is where the self-update status/log live. Tests override it.
var panelUpdateDir = "/var/lib/nft"

type githubReleaseInfo struct {
	Tag         string
	Name        string
	Notes       string
	HTMLURL     string
	PublishedAt string
}

type panelUpdateStatus struct {
	State      string `json:"state"`
	Current    string `json:"current,omitempty"`
	Target     string `json:"target,omitempty"`
	StartedAt  int64  `json:"started_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

var (
	fetchLatestReleaseFn = fetchLatestRelease
	startPanelUpgradeFn  = startPanelUpgradeDetached
	looksLikeInstallerFn = looksLikePanelInstaller

	latestRelMu    sync.Mutex
	latestRelCache *githubReleaseInfo
	latestRelAt    time.Time
	latestRelErr   error
)

func panelUpdateStatusPath() string { return filepath.Join(panelUpdateDir, panelUpdateStatusName) }
func panelUpdateLogPath() string    { return filepath.Join(panelUpdateDir, panelUpdateLogName) }

func normalizeVersionTag(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	return v
}

func parseSemverParts(v string) [3]int {
	var out [3]int
	v = normalizeVersionTag(v)
	if v == "" || v == "dev" || v == "latest" {
		return out
	}
	parts := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}

// compareSemver returns -1 if a<b, 0 if equal, 1 if a>b.
func compareSemver(a, b string) int {
	pa, pb := parseSemverParts(a), parseSemverParts(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func sameVersion(a, b string) bool {
	na, nb := normalizeVersionTag(a), normalizeVersionTag(b)
	if na == "" || nb == "" {
		return strings.TrimSpace(a) != "" && strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return compareSemver(a, b) == 0
}

func ghProxyPrefix() string {
	b, err := os.ReadFile(ghProxyFile)
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

func githubURL(raw string) string {
	return ghProxyPrefix() + raw
}

func fetchLatestRelease() (*githubReleaseInfo, error) {
	latestRelMu.Lock()
	if latestRelCache != nil && time.Since(latestRelAt) < latestReleaseCacheFor {
		c, e := latestRelCache, latestRelErr
		latestRelMu.Unlock()
		return c, e
	}
	latestRelMu.Unlock()

	info, err := fetchLatestReleaseUncached()

	latestRelMu.Lock()
	latestRelCache, latestRelErr, latestRelAt = info, err, time.Now()
	latestRelMu.Unlock()
	return info, err
}

func fetchLatestReleaseUncached() (*githubReleaseInfo, error) {
	api := githubURL("https://api.github.com/repos/" + agentRepo + "/releases/latest")
	body, err := httpGetUA(api, 20*time.Second, 1<<20)
	if err == nil {
		var raw struct {
			TagName     string `json:"tag_name"`
			Name        string `json:"name"`
			Body        string `json:"body"`
			HTMLURL     string `json:"html_url"`
			PublishedAt string `json:"published_at"`
		}
		if jerr := json.Unmarshal(body, &raw); jerr == nil && strings.HasPrefix(raw.TagName, "v") {
			notes := strings.TrimSpace(raw.Body)
			if utf8.RuneCountInString(notes) > maxReleaseNotes {
				r := []rune(notes)
				notes = string(r[:maxReleaseNotes]) + "…"
			}
			return &githubReleaseInfo{
				Tag: raw.TagName, Name: raw.Name, Notes: notes,
				HTMLURL: raw.HTMLURL, PublishedAt: raw.PublishedAt,
			}, nil
		}
	}
	// HTML latest page redirects to /releases/tag/vX.Y.Z
	page := githubURL("https://github.com/" + agentRepo + "/releases/latest")
	tag, herr := githubLatestTagFromRedirect(page)
	if herr != nil {
		if err != nil {
			return nil, fmt.Errorf("查询 GitHub 最新版失败: %w", err)
		}
		return nil, fmt.Errorf("查询 GitHub 最新版失败: %w", herr)
	}
	return &githubReleaseInfo{
		Tag:     tag,
		HTMLURL: "https://github.com/" + agentRepo + "/releases/tag/" + tag,
	}, nil
}

func githubLatestTagFromRedirect(url string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "kids-panel")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" && resp.Request != nil && resp.Request.URL != nil {
		loc = resp.Request.URL.String()
	}
	if i := strings.LastIndex(loc, "/tag/"); i >= 0 {
		tag := strings.Trim(loc[i+5:], "/ \r\n")
		if strings.HasPrefix(tag, "v") {
			return tag, nil
		}
	}
	return "", fmt.Errorf("无法从 GitHub 跳转解析版本")
}

func httpGetUA(url string, timeout time.Duration, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "kids-panel")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("GET %s: body too large", url)
	}
	return data, nil
}

func readPanelUpdateStatus() panelUpdateStatus {
	b, err := os.ReadFile(panelUpdateStatusPath())
	if err != nil {
		return panelUpdateStatus{State: "idle"}
	}
	var st panelUpdateStatus
	if json.Unmarshal(b, &st) != nil || st.State == "" {
		return panelUpdateStatus{State: "idle"}
	}
	return st
}

func writePanelUpdateStatus(st panelUpdateStatus) error {
	if err := os.MkdirAll(panelUpdateDir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := panelUpdateStatusPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, panelUpdateStatusPath())
}

func readUpdateLogTail() string {
	b, err := os.ReadFile(panelUpdateLogPath())
	if err != nil {
		return ""
	}
	if len(b) > maxUpdateLogTail {
		b = b[len(b)-maxUpdateLogTail:]
	}
	return stripANSI(string(b))
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				c := s[j]
				j++
				if c >= '@' && c <= '~' {
					break
				}
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func resolvePanelUpdateStatus(st panelUpdateStatus, current string) panelUpdateStatus {
	if st.State != "running" {
		return st
	}
	now := time.Now().Unix()
	if st.StartedAt > 0 && now-st.StartedAt > int64(panelUpdateStaleAfter.Seconds()) {
		st.State = "error"
		if st.Error == "" {
			st.Error = "升级超时，面板可能未完成重启。请刷新后重试，或 SSH 执行 nft-upgrade update"
		}
		st.FinishedAt = now
		_ = writePanelUpdateStatus(st)
		return st
	}
	if st.Target != "" && sameVersion(current, st.Target) {
		st.State = "success"
		if st.FinishedAt == 0 {
			st.FinishedAt = now
		}
		_ = writePanelUpdateStatus(st)
	}
	return st
}

func looksLikePanelInstaller(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 80 {
		return false
	}
	s := string(b)
	if !strings.HasPrefix(strings.TrimSpace(s), "#!") {
		return false
	}
	// Node-install overwrites nft-upgrade with a curl-to-/v1/install-agent wrapper.
	// That path rewrites the local daemon with --connect and breaks the panel.
	if strings.Contains(s, "do_update") && strings.Contains(s, "nft-server") {
		return true
	}
	return false
}

func startPanelUpgradeDetached(target, current string) error {
	if err := os.MkdirAll(panelUpdateDir, 0o755); err != nil {
		return err
	}
	script := filepath.Join(panelUpdateDir, panelUpdateScriptName)
	installSH := filepath.Join(panelUpdateDir, panelUpdateInstallSH)
	logPath := panelUpdateLogPath()
	statusPath := panelUpdateStatusPath()

	useLocal := looksLikeInstallerFn(nftUpgradePath)
	if !useLocal {
		raw := githubURL("https://raw.githubusercontent.com/" + agentRepo + "/main/install.sh")
		body, err := httpGetUA(raw, 30*time.Second, 1<<20)
		if err != nil {
			return fmt.Errorf("下载官方安装脚本失败: %w", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(body)), "#!") || !strings.Contains(string(body), "do_update") {
			return fmt.Errorf("下载的 install.sh 内容异常，拒绝执行")
		}
		if err := os.WriteFile(installSH, body, 0o755); err != nil {
			return err
		}
	}

	upgradeLine := "bash " + shellQuote(installSH) + " update --release " + shellQuote(target)
	if useLocal {
		upgradeLine = shellQuote(nftUpgradePath) + " update --release " + shellQuote(target)
	}

	body := fmt.Sprintf(`#!/usr/bin/env bash
set -uo pipefail
STATUS=%s
LOG=%s
TARGET=%s
sleep 2
rc=0
%s >>"$LOG" 2>&1 || rc=$?
if [[ "$rc" -eq 0 ]]; then
  cat >"$STATUS" <<EOF
{"state":"success","current":"%s","target":"%s","finished_at":$(date +%%s),"error":""}
EOF
else
  cat >"$STATUS" <<EOF
{"state":"error","current":"%s","target":"%s","finished_at":$(date +%%s),"error":"升级失败（退出码 $rc），请查看日志"}
EOF
fi
`,
		shellQuote(statusPath), shellQuote(logPath), shellQuote(target),
		upgradeLine,
		escapeJSONString(current), escapeJSONString(target),
		escapeJSONString(current), escapeJSONString(target),
	)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(logPath, []byte("开始升级到 "+target+"\n"), 0o644); err != nil {
		return err
	}

	if err := trySystemdRun(script); err == nil {
		return nil
	}
	cmd := exec.Command("bash", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir = panelUpdateDir
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法启动升级任务: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func trySystemdRun(script string) error {
	_ = exec.Command("systemctl", "reset-failed", "nft-panel-self-update.service").Run()
	cmd := exec.Command("systemd-run",
		"--unit=nft-panel-self-update",
		"--collect",
		"--property=Type=oneshot",
		"--description=kids panel self-update",
		script,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func (s *Server) apiGetPanelUpdate(w http.ResponseWriter, r *http.Request) {
	current := serverVersion()
	st := resolvePanelUpdateStatus(readPanelUpdateStatus(), current)

	refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	if refresh {
		latestRelMu.Lock()
		latestRelCache, latestRelAt = nil, time.Time{}
		latestRelMu.Unlock()
	}

	var latest *githubReleaseInfo
	var fetchErr error
	// Status-only polls reuse the cache so a 2s UI loop cannot burn the rate limit.
	if r.URL.Query().Get("status") == "1" {
		latestRelMu.Lock()
		latest = latestRelCache
		latestRelMu.Unlock()
	} else {
		latest, fetchErr = fetchLatestReleaseFn()
	}

	upToDate := false
	latestTag := ""
	notes, htmlURL, published := "", "", ""
	if latest != nil {
		latestTag = latest.Tag
		notes, htmlURL, published = latest.Notes, latest.HTMLURL, latest.PublishedAt
		cur := strings.TrimSpace(current)
		upToDate = cur != "" && cur != "dev" && latestTag != "" && compareSemver(cur, latestTag) >= 0
	}

	out := map[string]any{
		"current":      current,
		"latest":       latestTag,
		"up_to_date":   upToDate,
		"notes":        notes,
		"html_url":     htmlURL,
		"published_at": published,
		"can_upgrade":  true,
		"update": map[string]any{
			"state":       st.State,
			"current":     st.Current,
			"target":      st.Target,
			"started_at":  st.StartedAt,
			"finished_at": st.FinishedAt,
			"error":       st.Error,
			"log":         readUpdateLogTail(),
		},
	}
	if fetchErr != nil {
		out["check_error"] = fetchErr.Error()
	}
	jsonOK(w, out)
}

func (s *Server) apiStartPanelUpdate(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	current := serverVersion()
	st := resolvePanelUpdateStatus(readPanelUpdateStatus(), current)
	if st.State == "running" {
		jsonErr(w, http.StatusConflict, "升级正在进行中，请稍候")
		return
	}

	latest, err := fetchLatestReleaseFn()
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "查询最新版本失败: "+err.Error())
		return
	}
	target := latest.Tag
	if target == "" {
		jsonErr(w, http.StatusBadGateway, "GitHub 未返回版本号")
		return
	}
	if strings.TrimSpace(current) != "" && current != "dev" && compareSemver(current, target) >= 0 {
		jsonErr(w, http.StatusBadRequest, "已是最新版本 "+target)
		return
	}

	next := panelUpdateStatus{
		State:     "running",
		Current:   current,
		Target:    target,
		StartedAt: time.Now().Unix(),
	}
	if err := writePanelUpdateStatus(next); err != nil {
		jsonErr(w, http.StatusInternalServerError, "无法写入升级状态: "+err.Error())
		return
	}
	if err := startPanelUpgradeFn(target, current); err != nil {
		next.State = "error"
		next.Error = err.Error()
		next.FinishedAt = time.Now().Unix()
		_ = writePanelUpdateStatus(next)
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u != nil {
		db.WriteAudit(s.DB, u.ID, "settings.panel_update", target, current+" -> "+target)
	}
	jsonOK(w, map[string]any{
		"ok":     true,
		"state":  "running",
		"target": target,
	})
}
