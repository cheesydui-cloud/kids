package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"nft/internal/cloudflare"
	"nft/internal/db"
	"nft/internal/resolver"
)

func settingsCFTTL(s *Server) int {
	cfTTLStr, _ := db.GetSetting(s.DB, "cf_ttl")
	ttl := 1
	if n, err := strconv.Atoi(strings.TrimSpace(cfTTLStr)); err == nil && n > 0 {
		ttl = n
	}
	return ttl
}

func (s *Server) apiListCFRecords(w http.ResponseWriter, r *http.Request) {
	cli, zid, err := s.cfClientAndZone(r.Context(), "")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	recs, err := cli.ListARecords(r.Context(), zid)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"zone_id": zid,
		"records": recs,
		"cf_ttl":  settingsCFTTL(s),
	})
}

func (s *Server) apiCreateCFRecord(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		TTL     int    `json:"ttl"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(strings.ToLower(body.Name))
	ip := strings.TrimSpace(body.Content)
	if name == "" || ip == "" {
		jsonErr(w, http.StatusBadRequest, "记录名和 IPv4 不能为空")
		return
	}
	if !resolver.PlausibleHostname(name) {
		jsonErr(w, http.StatusBadRequest, "记录名不是合法域名")
		return
	}
	if !cloudflare.IsIPv4(ip) {
		jsonErr(w, http.StatusBadRequest, "内容必须是 IPv4 地址")
		return
	}
	ttl := body.TTL
	if ttl < 1 {
		ttl = settingsCFTTL(s)
	}
	cli, zid, err := s.cfClientAndZone(r.Context(), "")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, err := cli.UpsertARecord(r.Context(), zid, name, ip, ttl)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if u != nil {
		db.WriteAudit(s.DB, u.ID, "settings.cf_record_upsert", rec.Name, rec.Content)
	}
	jsonOK(w, map[string]any{"ok": true, "record": rec})
}

func (s *Server) apiDeleteCFRecord(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		jsonErr(w, http.StatusBadRequest, "缺少记录 ID")
		return
	}
	cli, zid, err := s.cfClientAndZone(r.Context(), "")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := cli.DeleteDNSRecord(r.Context(), zid, id); err != nil {
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if u != nil {
		db.WriteAudit(s.DB, u.ID, "settings.cf_record_delete", id, "")
	}
	jsonOK(w, map[string]any{"ok": true})
}
