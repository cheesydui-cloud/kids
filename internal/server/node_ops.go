package server

import (
	"net/http"
	"strconv"

	"nft/internal/db"
)

// adminNodeView promotes the admin-only node operations metadata that db.Node
// keeps hidden (json:"-") so it can never leak to tenant responses.
type adminNodeView struct {
	*db.Node
	GroupID          int64  `json:"group_id"`
	GroupName        string `json:"group_name"`
	Remark           string `json:"remark"`
	ExpiresAt        int64  `json:"expires_at"`
	MonthlyCostCents int64  `json:"monthly_cost_cents"`
}

func nodeAdminView(n *db.Node) adminNodeView {
	if n == nil {
		return adminNodeView{}
	}
	return adminNodeView{
		Node:             n,
		GroupID:          n.GroupID,
		GroupName:        n.GroupName,
		Remark:           n.Remark,
		ExpiresAt:        n.ExpiresAt,
		MonthlyCostCents: n.MonthlyCostCents,
	}
}

// apiListNodeFolders returns the admin node folders plus the ungrouped count.
func (s *Server) apiListNodeFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := db.ListNodeFolders(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if folders == nil {
		folders = []*db.Folder{}
	}
	ungrouped, err := db.UngroupedNodeCount(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"folders": folders, "ungrouped": ungrouped})
}

func (s *Server) apiCreateNodeFolder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	f, err := db.CreateNodeFolder(s.DB, body.Name)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if u := userFromCtx(r.Context()); u != nil {
		db.WriteAudit(s.DB, u.ID, "node_folder.create", strconv.FormatInt(f.ID, 10), f.Name)
	}
	jsonOK(w, f)
}

func (s *Server) apiRenameNodeFolder(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := db.RenameNodeFolder(s.DB, id, body.Name); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if u := userFromCtx(r.Context()); u != nil {
		db.WriteAudit(s.DB, u.ID, "node_folder.rename", strconv.FormatInt(id, 10), body.Name)
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) apiDeleteNodeFolder(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := db.DeleteNodeFolder(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u := userFromCtx(r.Context()); u != nil {
		db.WriteAudit(s.DB, u.ID, "node_folder.delete", strconv.FormatInt(id, 10), "")
	}
	jsonOK(w, map[string]any{"ok": true})
}

// apiUpdateNodeOps writes the admin-only operations metadata for one node:
// remark, renewal date, monthly cost and (optionally) folder membership.
// Omitted JSON fields keep their current value.
func (s *Server) apiUpdateNodeOps(w http.ResponseWriter, r *http.Request) {
	id, err := urlParamInt64(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Remark           *string `json:"remark"`
		ExpiresAt        *int64  `json:"expires_at"`
		MonthlyCostCents *int64  `json:"monthly_cost_cents"`
		GroupID          *int64  `json:"group_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cur, err := db.GetNode(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "节点不存在")
		return
	}
	remark, expiresAt, cost := cur.Remark, cur.ExpiresAt, cur.MonthlyCostCents
	if body.Remark != nil {
		remark = *body.Remark
	}
	if body.ExpiresAt != nil {
		expiresAt = *body.ExpiresAt
	}
	if body.MonthlyCostCents != nil {
		cost = *body.MonthlyCostCents
	}
	if err := db.UpdateNodeOpsFields(s.DB, id, remark, expiresAt, cost); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body.GroupID != nil {
		if err := db.SetNodeFolder(s.DB, id, *body.GroupID); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if u := userFromCtx(r.Context()); u != nil {
		db.WriteAudit(s.DB, u.ID, "node.ops_update", strconv.FormatInt(id, 10), cur.Name)
	}
	updated, err := db.GetNode(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, nodeAdminView(updated))
}
