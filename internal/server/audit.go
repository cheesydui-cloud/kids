package server

import (
	"net/http"
	"strconv"

	"nft/internal/db"
)

// apiListAuditLogs returns one page of audit records (admin only). Filters:
// q (action/target/payload/username), user_id, from, to, page, page_size.
func (s *Server) apiListAuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ := strconv.Atoi(q.Get("page_size"))
	if size <= 0 || size > 200 {
		size = 50
	}
	f := db.AuditFilter{
		Query: q.Get("q"),
		From:  queryInt64(q.Get("from")),
		To:    queryInt64(q.Get("to")),
		Limit: size,
		Offset: (page - 1) * size,
	}
	if uid := queryInt64(q.Get("user_id")); uid > 0 {
		f.UserID = uid
	}
	logs, total, err := db.ListAuditLogs(s.DB, f)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []db.AuditLog{}
	}
	jsonOK(w, map[string]any{
		"logs": logs, "total": total, "page": page, "page_size": size,
	})
}

func queryInt64(raw string) int64 {
	n, _ := strconv.ParseInt(raw, 10, 64)
	return n
}
