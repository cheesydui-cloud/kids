package server

import (
	"net/http"
	"strings"

	"nft/internal/db"
)

// apiNodeLogs pulls the connected agent's recent filtered log ring.
// Offline nodes get a stable Chinese error and no journal is stored.
func (s *Server) apiNodeLogs(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	n, err := db.GetNode(s.DB, id)
	if err != nil || n == nil {
		jsonErr(w, http.StatusNotFound, "节点不存在")
		return
	}
	if s.Hub == nil || !s.Hub.IsOnline(id) {
		jsonErr(w, http.StatusConflict, "节点未连接")
		return
	}
	lines, err := s.Hub.PullLogs(id)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "未连接") {
			jsonErr(w, http.StatusConflict, "节点未连接")
			return
		}
		jsonErr(w, http.StatusBadGateway, msg)
		return
	}
	jsonOK(w, map[string]any{"lines": lines})
}
