package server

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"

	"nft/internal/db"
)

func (s *Server) apiBackupStatus(w http.ResponseWriter, r *http.Request) {
	dir := ""
	if s.DataDir != "" {
		dir = filepath.Join(s.DataDir, "backups")
	}
	st := db.ReadBackupStatus(dir)
	jsonOK(w, map[string]any{
		"dir":              st.Dir,
		"count":            st.Count,
		"latest_name":      st.LatestName,
		"latest_unix":      st.LatestUnix,
		"last_error":       st.LastError,
		"keep":             st.Keep,
		"interval_seconds": st.IntervalSeconds,
		"key_file":         db.SubTokenKeyFile,
		"key_source":       db.SubTokenKeySource(s.DB),
		"restore":          "停掉 nft-server。把 backups 里校验过的 panel-日期.db 换成 panel.db，同目录的 sub_token.key 要一起留着（或设置相同的 NFT_SUB_TOKEN_KEY）。再启动。不要只拷数据库文件到一台没有这把密钥的新机器，否则已有订阅地址显示不出来，用户需要重置链接。每天的 panel-*.db 不含这把密钥。搬家包会带上它。",
	})
}

func (s *Server) apiMyCreateRequest(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var body struct {
		Kind string `json:"kind"`
		Note string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	kind := strings.TrimSpace(body.Kind)
	if !db.ValidRequestKind(kind) {
		jsonErr(w, http.StatusBadRequest, "类型只能是续期或加量")
		return
	}
	row, already, err := db.CreateUserRequest(s.DB, u.ID, kind, body.Note)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "提交失败")
		return
	}
	if !already {
		_ = db.WriteAudit(s.DB, u.ID, "user.request", kind, row.Note)
	}
	jsonOK(w, map[string]any{"request": row, "already": already})
}

func (s *Server) apiCloseUserRequest(w http.ResponseWriter, r *http.Request) {
	admin := userFromCtx(r.Context())
	userID, err := urlParamInt64(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "bad id")
		return
	}
	reqID, err := urlParamInt64(r, "reqID")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := db.CloseUserRequest(s.DB, userID, reqID); err != nil {
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "申请不存在或已处理")
			return
		}
		jsonErr(w, http.StatusInternalServerError, "处理失败")
		return
	}
	_ = db.WriteAudit(s.DB, admin.ID, "user.request_done", "", "")
	jsonOK(w, map[string]any{"ok": true})
}
