package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"

	"nft/internal/db"
)

const listProbeMaxNodes = 80

type listProbeItem struct {
	NodeID  int64  `json:"node_id"`
	OK      bool   `json:"ok"`
	Latency int    `json:"latency_ms"`
	Error   string `json:"error,omitempty"`
	Hop     string `json:"hop,omitempty"`
}

// apiProbeNodeList measures the currently visible lines with the same hop
// probe the detail page uses: an agent dials the next hop's deployed port.
// Results are not stored. The client drops them after a few minutes.
func (s *Server) apiProbeNodeList(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &body); err != nil {
		jsonErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	ids := uniquePositiveIDs(body.IDs)
	if len(ids) == 0 {
		jsonErr(w, http.StatusBadRequest, "请选择要测的节点")
		return
	}
	if len(ids) > listProbeMaxNodes {
		jsonErr(w, http.StatusBadRequest, "一次最多测 80 个")
		return
	}
	items := make([]listProbeItem, len(ids))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i int, id int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items[i] = s.probeListNode(id)
		}(i, id)
	}
	wg.Wait()
	jsonOK(w, map[string]any{"items": items, "ttl_sec": 180})
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (s *Server) probeListNode(id int64) listProbeItem {
	n, err := db.GetNode(s.DB, id)
	if err != nil || n == nil {
		return listProbeItem{NodeID: id, Error: "节点不存在"}
	}
	if n.NodeType == "composite" {
		return s.probeCompositeList(n)
	}
	if n.NodeType == "self" {
		return listProbeItem{NodeID: id, Error: "不能测面板自身"}
	}
	next, err := db.NextHopIDs(s.DB, id)
	if err != nil {
		return listProbeItem{NodeID: id, Error: err.Error()}
	}
	if len(next) > 0 {
		return s.probeOutward(id, next)
	}
	prev, err := db.PrevHopIDs(s.DB, id)
	if err != nil {
		return listProbeItem{NodeID: id, Error: err.Error()}
	}
	if len(prev) > 0 {
		return s.probeInward(id, prev)
	}
	return listProbeItem{NodeID: id, Error: "没有已下发的下一跳"}
}

func (s *Server) probeCompositeList(n *db.Node) listProbeItem {
	hops, err := db.ListNodeHops(s.DB, n.ID)
	if err != nil {
		return listProbeItem{NodeID: n.ID, Error: err.Error()}
	}
	if len(hops) < 2 {
		return listProbeItem{NodeID: n.ID, Error: "组合节点不足两跳"}
	}
	maxLat := 0
	slow := ""
	for i := 0; i < len(hops)-1; i++ {
		from, to := hops[i].HopNodeID, hops[i+1].HopNodeID
		name := hopDisplayName(s.DB, to)
		lat, perr := s.probeDeployedHop(from, to)
		if perr != nil {
			return listProbeItem{NodeID: n.ID, Error: name + "：" + perr.Error(), Hop: name}
		}
		if lat >= maxLat {
			maxLat = lat
			slow = name
		}
	}
	return listProbeItem{NodeID: n.ID, OK: true, Latency: maxLat, Hop: slow}
}

func (s *Server) probeOutward(id int64, next []int64) listProbeItem {
	if len(next) > 4 {
		next = next[:4]
	}
	maxLat := 0
	slow := ""
	for _, to := range next {
		name := hopDisplayName(s.DB, to)
		lat, err := s.probeDeployedHop(id, to)
		if err != nil {
			return listProbeItem{NodeID: id, Error: name + "：" + err.Error(), Hop: name}
		}
		if lat >= maxLat {
			maxLat = lat
			slow = name
		}
	}
	return listProbeItem{NodeID: id, OK: true, Latency: maxLat, Hop: slow}
}

func (s *Server) probeInward(id int64, prev []int64) listProbeItem {
	if len(prev) > 4 {
		prev = prev[:4]
	}
	maxLat := 0
	slow := ""
	for _, from := range prev {
		name := hopDisplayName(s.DB, from)
		lat, err := s.probeDeployedHop(from, id)
		if err != nil {
			return listProbeItem{NodeID: id, Error: name + "：" + err.Error(), Hop: name}
		}
		if lat >= maxLat {
			maxLat = lat
			slow = name
		}
	}
	return listProbeItem{NodeID: id, OK: true, Latency: maxLat, Hop: slow}
}

// probeDeployedHop dials one already-deployed listen port, the same way
// probeHopEndpoint does, and keeps only the first port so a list sweep stays short.
func (s *Server) probeDeployedHop(fromID, toID int64) (int, error) {
	from, err := db.GetNode(s.DB, fromID)
	if err != nil || from == nil {
		return 0, fmt.Errorf("起始节点不存在")
	}
	to, err := db.GetNode(s.DB, toID)
	if err != nil || to == nil {
		return 0, fmt.Errorf("下一跳不存在")
	}
	if from.Disabled || from.Online != 1 {
		name := from.Name
		if name == "" {
			name = fmt.Sprintf("#%d", from.ID)
		}
		return 0, fmt.Errorf("%s 离线", name)
	}
	ports, err := db.ListenPortsOnNodeFromPrev(s.DB, fromID, toID)
	if err != nil {
		return 0, err
	}
	if len(ports) == 0 {
		return 0, fmt.Errorf("没有已下发的中继端口")
	}
	target, err := hopRelayTarget(to, ports[0])
	if err != nil {
		return 0, err
	}
	if s.Hub == nil {
		return 0, fmt.Errorf("节点未连接")
	}
	ack, err := s.Hub.SendProbe(fromID, target)
	if err != nil {
		return 0, err
	}
	if !ack.OK {
		if ack.Error != "" {
			return 0, fmt.Errorf("%s", ack.Error)
		}
		return 0, fmt.Errorf("不通")
	}
	return ack.Latency, nil
}

func hopDisplayName(d *sql.DB, id int64) string {
	n, err := db.GetNode(d, id)
	if err != nil || n == nil || n.Name == "" {
		return fmt.Sprintf("#%d", id)
	}
	return n.Name
}
