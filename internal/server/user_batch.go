package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"nft/internal/db"
)

const maxBatchUsers = 200

// apiBatchUsers applies one account action to many users.
// Actions: renew, disable, enable, quota, rate, reset_traffic.
func (s *Server) apiBatchUsers(w http.ResponseWriter, r *http.Request) {
	actor := userFromCtx(r.Context())
	var body struct {
		IDs            []int64 `json:"ids"`
		Action         string  `json:"action"`
		Days           int     `json:"days"`
		TrafficQuotaGB float64 `json:"traffic_quota_gb"`
		BillingRate    float64 `json:"billing_rate"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(body.IDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "请先勾选用户")
		return
	}
	if len(body.IDs) > maxBatchUsers {
		jsonErr(w, http.StatusBadRequest, "一次最多 200 个用户")
		return
	}
	action := strings.TrimSpace(body.Action)
	switch action {
	case "renew", "disable", "enable", "quota", "rate", "reset_traffic":
	default:
		jsonErr(w, http.StatusBadRequest, "不支持的批量操作")
		return
	}
	if action == "renew" {
		if body.Days <= 0 {
			body.Days = 30
		}
		if body.Days > 3650 {
			jsonErr(w, http.StatusBadRequest, "续期天数无效")
			return
		}
	}
	if action == "quota" && body.TrafficQuotaGB < 0 {
		jsonErr(w, http.StatusBadRequest, "流量配额无效")
		return
	}
	if action == "rate" && body.BillingRate <= 0 {
		jsonErr(w, http.StatusBadRequest, "倍率须大于 0")
		return
	}

	nowUnix := time.Now().Unix()
	updated := 0
	skipped := 0
	seenNode := map[int64]bool{}
	var touched []int64
	noteNodes := func(id int64) {
		nodes, err := db.DistinctUserNodes(s.DB, id)
		if err != nil {
			return
		}
		for _, n := range nodes {
			if seenNode[n] {
				continue
			}
			seenNode[n] = true
			touched = append(touched, n)
		}
	}

	for _, id := range body.IDs {
		target, err := db.GetUserByID(s.DB, id)
		if err != nil {
			skipped++
			continue
		}
		switch action {
		case "disable":
			if target.ID == actor.ID || target.Disabled {
				skipped++
				continue
			}
			if err := db.SetUserDisabled(s.DB, id, true, "管理员手动禁用"); err != nil {
				skipped++
				continue
			}
			noteNodes(id)
		case "enable":
			if !target.Disabled {
				skipped++
				continue
			}
			if err := db.SetUserDisabled(s.DB, id, false, ""); err != nil {
				skipped++
				continue
			}
			noteNodes(id)
		case "renew":
			if target.Role == "admin" {
				skipped++
				continue
			}
			cur := int64(0)
			if target.ExpiresAt.Valid {
				cur = target.ExpiresAt.Int64
			}
			next := db.ExtendExpiry(cur, nowUnix, body.Days)
			if _, err := s.DB.Exec(`UPDATE users SET expires_at=? WHERE id=?`, next, id); err != nil {
				skipped++
				continue
			}
			noteNodes(id)
		case "quota":
			if target.Role == "admin" {
				skipped++
				continue
			}
			quotaBytes := int64(body.TrafficQuotaGB * 1073741824)
			if quotaBytes < 0 {
				quotaBytes = 0
			}
			if _, err := s.DB.Exec(`UPDATE users SET traffic_quota_bytes=? WHERE id=?`, quotaBytes, id); err != nil {
				skipped++
				continue
			}
			s.enforceUserQuota(id)
			noteNodes(id)
		case "rate":
			if target.Role == "admin" {
				skipped++
				continue
			}
			if _, err := s.DB.Exec(`UPDATE users SET billing_rate=? WHERE id=?`, body.BillingRate, id); err != nil {
				skipped++
				continue
			}
			s.enforceUserQuota(id)
			noteNodes(id)
		case "reset_traffic":
			if target.Role == "admin" {
				skipped++
				continue
			}
			if err := db.ResetAllUserTraffic(s.DB, id); err != nil {
				skipped++
				continue
			}
			if fresh, err := db.GetUserByID(s.DB, id); err == nil &&
				fresh.Disabled && fresh.DisableReason.Valid && fresh.DisableReason.String == "流量超额" {
				_ = db.SetUserDisabled(s.DB, id, false, "")
			}
			noteNodes(id)
		}
		updated++
	}
	if len(touched) > 0 {
		s.apiDispatchFanout(touched)
	}
	db.WriteAudit(s.DB, actor.ID, "user.batch_"+action, strconv.Itoa(updated), strconv.Itoa(skipped))
	jsonOK(w, map[string]any{"ok": true, "updated": updated, "skipped": skipped})
}
