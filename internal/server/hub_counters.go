package server

import (
	"database/sql"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"nft/internal/db"
	"nft/internal/wsproto"
)

const (
	// lastSeenMinInterval skips last_seen writes that are closer together than
	// this. Agents ping about every 10s; nodeStaleOnlineTTL is 45s, so a write
	// every 20s still refreshes the heartbeat before a live node looks stale.
	lastSeenMinInterval = 20 * time.Second
	// counterSnapTTL bounds how long a structural counter lookup may be reused
	// even if a writer forgot to bump the data epoch.
	counterSnapTTL = 15 * time.Second
)

// accountedSample is one proto/port after same-key samples in a queued batch
// have been summed. sum* is what billing and the speed cache add. last* is
// the newest sample, stored on rule_hops.last_bytes*.
type accountedSample struct {
	proto            string
	port             int
	sumUp, sumDown   int64
	lastUp, lastDown int64
}

type counterSnap struct {
	epoch          int64
	loaded         time.Time
	hopMap         map[string]*db.RuleHop
	ruleMap        map[int64]*db.Rule
	multipliers    map[int64]float64
	segFirst       map[int64]map[int]int64
	exitSet        map[db.UserExitKey]bool
	maxPos         map[int64]int
	unidirectional bool
}

type counterSnapCache struct {
	mu     sync.Mutex
	byNode map[int64]*counterSnap
}

func (ac *agentConn) maybeTouchLastSeen(d *sql.DB) {
	now := time.Now().UnixNano()
	prev := ac.lastSeenUnix.Load()
	if prev != 0 && time.Duration(now-prev) < lastSeenMinInterval {
		return
	}
	if !ac.lastSeenUnix.CompareAndSwap(prev, now) {
		return
	}
	if err := db.TouchNodeLastSeen(d, ac.nodeID); err != nil {
		ac.lastSeenUnix.CompareAndSwap(now, prev)
		log.Printf("hub: TouchNodeLastSeen node %d: %v", ac.nodeID, err)
	}
}

// enqueueCounters merges samples by proto/port and wakes the counter worker.
// It never blocks the reader. Bytes are normalized (legacy bytes_delta folded
// into uplink) before they are summed.
func (ac *agentConn) enqueueCounters(samples []wsproto.CounterSample) {
	if len(samples) == 0 || ac.counterWake == nil {
		return
	}
	ac.counterMu.Lock()
	if ac.counterIndex == nil {
		ac.counterIndex = map[string]int{}
	}
	for _, s := range samples {
		up, down := s.BytesUp, s.BytesDown
		if up == 0 && down == 0 && s.BytesDelta > 0 {
			up = s.BytesDelta
		}
		key := s.Proto + "/" + strconv.Itoa(s.ListenPort)
		if i, ok := ac.counterIndex[key]; ok {
			cur := &ac.counterPending[i]
			cur.sumUp += up
			cur.sumDown += down
			cur.lastUp = up
			cur.lastDown = down
			continue
		}
		ac.counterIndex[key] = len(ac.counterPending)
		ac.counterPending = append(ac.counterPending, accountedSample{
			proto: s.Proto, port: s.ListenPort,
			sumUp: up, sumDown: down, lastUp: up, lastDown: down,
		})
	}
	ac.counterMu.Unlock()
	select {
	case ac.counterWake <- struct{}{}:
	default:
	}
}

func (h *Hub) counterLoop(ac *agentConn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hub: counterLoop panic for node %d: %v", ac.nodeID, r)
			ac.signalClose()
		}
	}()
	for {
		select {
		case <-ac.closed:
			h.flushPendingCounters(ac)
			return
		case <-ac.counterWake:
			h.flushPendingCounters(ac)
		}
	}
}

func (h *Hub) flushPendingCounters(ac *agentConn) {
	for {
		ac.counterMu.Lock()
		batch := ac.counterPending
		ac.counterPending = nil
		ac.counterIndex = map[string]int{}
		ac.counterMu.Unlock()
		if len(batch) == 0 {
			return
		}
		h.applyAccounted(ac.nodeID, batch)
	}
}

func normalizeAccounted(samples []wsproto.CounterSample) []accountedSample {
	out := make([]accountedSample, len(samples))
	for i, s := range samples {
		up, down := s.BytesUp, s.BytesDown
		if up == 0 && down == 0 && s.BytesDelta > 0 {
			up = s.BytesDelta
		}
		out[i] = accountedSample{
			proto: s.Proto, port: s.ListenPort,
			sumUp: up, sumDown: down, lastUp: up, lastDown: down,
		}
	}
	return out
}

// applyCounters folds one counters frame into hop, user, and node ledgers.
// Tests and any in-process caller stay synchronous; the websocket reader
// queues through enqueueCounters instead.
func (h *Hub) applyCounters(nodeID int64, samples []wsproto.CounterSample) {
	h.applyAccounted(nodeID, normalizeAccounted(samples))
}

// applyAccounted bills a batch. A failed structural lookup still commits the
// node's raw/daily/hourly totals and skips hop and user billing, so a blip
// cannot advance rule_hops while leaving the user ledger behind.
func (h *Hub) applyAccounted(nodeID int64, samples []accountedSample) {
	var rawAdd int64
	for _, s := range samples {
		rawAdd += s.sumUp + s.sumDown
	}

	snap, err := h.loadCounterSnap(nodeID)
	if err != nil {
		log.Printf("hub: node %d counters lookup failed, recording raw traffic only: %v", nodeID, err)
		h.flushRawTraffic(nodeID, rawAdd)
		h.recordSpeed(nodeID, samples, nil, nil)
		return
	}

	type userNode struct{ userID, nodeID int64 }
	touched := map[userNode]bool{}

	ownerSet := map[int64]bool{}
	for _, r := range snap.ruleMap {
		if r != nil && r.OwnerID.Valid {
			ownerSet[r.OwnerID.Int64] = true
		}
	}
	ownerIDs := make([]int64, 0, len(ownerSet))
	for id := range ownerSet {
		ownerIDs = append(ownerIDs, id)
	}
	if len(ownerIDs) > 0 {
		loaded, err := db.GetUsersByIDs(h.DB, ownerIDs)
		if err != nil {
			log.Printf("hub: node %d load users for counters: %v", nodeID, err)
		} else {
			for uid, u := range loaded {
				if u == nil {
					continue
				}
				if reset, _ := db.CheckAndResetTrafficCycle(h.DB, u); reset {
					if u.Disabled && u.DisableReason.Valid && u.DisableReason.String == "流量超额" {
						_ = db.SetUserDisabled(h.DB, uid, false, "")
					}
					if nodes, err := db.DistinctUserNodes(h.DB, uid); err == nil && h.Redispatch != nil {
						go h.Redispatch(nodes)
					}
				}
			}
		}
	}

	type hopWrite struct{ lastBytes, lastUp, lastDown, addTotal, addBilled int64 }
	hopWrites := map[int64]*hopWrite{}
	userNodeAdds := map[userNode]int64{}
	userAdds := map[int64]int64{}
	ruleExitAdds := map[int64]int64{}
	exitAdds := map[db.UserExitKey]int64{}
	var unmatched int
	var unmatchedPorts []string

	for _, s := range samples {
		totalDelta := s.sumUp + s.sumDown
		billedDelta := totalDelta
		if snap.unidirectional {
			billedDelta = s.sumUp
		}
		key := s.proto + "/" + strconv.Itoa(s.port)
		rh, ok := snap.hopMap[key]
		if !ok {
			unmatched++
			if len(unmatchedPorts) < 8 {
				unmatchedPorts = append(unmatchedPorts, key)
			}
			continue
		}
		r := snap.ruleMap[rh.RuleID]
		lastTotal := s.lastUp + s.lastDown

		if r != nil && r.OwnerID.Valid && totalDelta > 0 && len(snap.exitSet) > 0 && rh.Position == snap.maxPos[rh.RuleID] {
			ek := db.UserExitKey{UserID: r.OwnerID.Int64, Host: r.ExitHost, Port: r.ExitPort}
			if snap.exitSet[ek] {
				exitAdds[ek] += totalDelta
				touched[userNode{ek.UserID, nodeID}] = true
			}
		}

		billedBase := billedDelta
		var userID int64
		hasOwner := r != nil && r.OwnerID.Valid && totalDelta > 0
		if hasOwner {
			userID = r.OwnerID.Int64
			entryMult, ok := snap.multipliers[r.NodeID]
			if !ok || entryMult < 0 {
				entryMult = 1.0
			}
			billedBase = int64(math.Round(float64(billedDelta) * entryMult))
		}

		w := hopWrites[rh.ID]
		if w == nil {
			w = &hopWrite{}
			hopWrites[rh.ID] = w
		}
		w.lastBytes = lastTotal
		w.lastUp = s.lastUp
		w.lastDown = s.lastDown
		w.addTotal += totalDelta
		if rh.Position == 0 {
			w.addBilled += billedBase
		}
		if !hasOwner {
			continue
		}
		if via, ok := snap.segFirst[rh.RuleID][rh.Position]; ok {
			userNodeAdds[userNode{userID, via}] += billedDelta
			touched[userNode{userID, via}] = true
		}
		if len(snap.maxPos) > 0 && rh.Position == snap.maxPos[rh.RuleID] && totalDelta > 0 {
			ruleExitAdds[rh.RuleID] += totalDelta
			userAdds[userID] += totalDelta
		}
	}
	if unmatched > 0 {
		log.Printf("hub: node %d counters: %d samples matched no rule_hop (ports %s)", nodeID, unmatched, strings.Join(unmatchedPorts, ","))
	}

	hops := make([]db.RuleHopCounterUpdate, 0, len(hopWrites))
	for id, w := range hopWrites {
		hops = append(hops, db.RuleHopCounterUpdate{
			ID: id, LastBytes: w.lastBytes, LastUp: w.lastUp, LastDown: w.lastDown,
			AddTotal: w.addTotal, AddBilled: w.addBilled,
		})
	}
	grants := make([]db.UserNodeTrafficUpdate, 0, len(userNodeAdds))
	for un, delta := range userNodeAdds {
		grants = append(grants, db.UserNodeTrafficUpdate{UserID: un.userID, NodeID: un.nodeID, Delta: delta})
	}
	users := make([]db.UserTrafficUpdate, 0, len(userAdds))
	for uid, delta := range userAdds {
		users = append(users, db.UserTrafficUpdate{UserID: uid, Cycle: delta, Total: delta})
	}
	exits := make([]db.IDDelta, 0, len(ruleExitAdds))
	for id, delta := range ruleExitAdds {
		exits = append(exits, db.IDDelta{ID: id, Delta: delta})
	}
	ledgers := make([]db.LandingExitTrafficUpdate, 0, len(exitAdds))
	for k, delta := range exitAdds {
		ledgers = append(ledgers, db.LandingExitTrafficUpdate{UserID: k.UserID, Host: k.Host, Port: k.Port, Delta: delta})
	}

	if len(hops) > 0 || len(grants) > 0 || len(users) > 0 || len(exits) > 0 || len(ledgers) > 0 || rawAdd > 0 {
		if tx, err := h.DB.Begin(); err != nil {
			log.Printf("hub: node %d counters tx begin: %v", nodeID, err)
		} else {
			ok := true
			fail := func(what string, err error) {
				log.Printf("hub: node %d counters %s: %v", nodeID, what, err)
				ok = false
			}
			if rawAdd > 0 && ok {
				if err := db.AddNodeRawTraffic(tx, nodeID, rawAdd); err != nil {
					fail("raw traffic add", err)
				}
			}
			if rawAdd > 0 && ok {
				if err := db.AddNodeDailyRawTraffic(tx, nodeID, rawAdd); err != nil {
					fail("daily raw traffic add", err)
				}
			}
			if rawAdd > 0 && ok {
				if err := db.AddHourlyRawTraffic(tx, rawAdd); err != nil {
					fail("hourly raw traffic add", err)
				}
			}
			if ok {
				if err := db.UpdateRuleHopCounters(tx, hops); err != nil {
					fail("rule_hop batch", err)
				}
			}
			if ok {
				if err := db.UpdateUserNodeTrafficBatch(tx, grants); err != nil {
					fail("per-node traffic batch", err)
				}
			}
			if ok {
				if err := db.UpdateUserTrafficBatch(tx, users); err != nil {
					fail("user traffic batch", err)
				}
			}
			if ok {
				if err := db.AddUserDailyTrafficBatch(tx, users); err != nil {
					fail("user daily traffic batch", err)
				}
			}
			if ok {
				if err := db.UpdateRuleExitBytesBatch(tx, exits); err != nil {
					fail("rule exit batch", err)
				}
			}
			if ok {
				if err := db.UpdateLandingExitUsedBatch(tx, ledgers, time.Now().Unix()); err != nil {
					fail("landing exit batch", err)
				}
			}
			if ok {
				if err := tx.Commit(); err != nil {
					log.Printf("hub: node %d counters tx commit: %v", nodeID, err)
				}
			} else {
				_ = tx.Rollback()
			}
		}
	}

	h.recordSpeed(nodeID, samples, snap.hopMap, snap.ruleMap)

	if h.OnTrafficUpdate != nil && len(touched) > 0 {
		pairs := make([]userNode, 0, len(touched))
		for un := range touched {
			pairs = append(pairs, un)
		}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("hub: OnTrafficUpdate panic: %v", r)
				}
			}()
			for _, un := range pairs {
				h.OnTrafficUpdate(un.userID, un.nodeID)
			}
		}()
	}
}

func (h *Hub) flushRawTraffic(nodeID, rawAdd int64) {
	if rawAdd <= 0 {
		return
	}
	tx, err := h.DB.Begin()
	if err != nil {
		log.Printf("hub: node %d raw traffic tx begin: %v", nodeID, err)
		return
	}
	if err := db.AddNodeRawTraffic(tx, nodeID, rawAdd); err != nil {
		_ = tx.Rollback()
		log.Printf("hub: node %d raw traffic add: %v", nodeID, err)
		return
	}
	if err := db.AddNodeDailyRawTraffic(tx, nodeID, rawAdd); err != nil {
		_ = tx.Rollback()
		log.Printf("hub: node %d daily raw traffic add: %v", nodeID, err)
		return
	}
	if err := db.AddHourlyRawTraffic(tx, rawAdd); err != nil {
		_ = tx.Rollback()
		log.Printf("hub: hourly raw traffic add: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("hub: node %d raw traffic tx commit: %v", nodeID, err)
	}
}

func (h *Hub) recordSpeed(nodeID int64, samples []accountedSample, hopMap map[string]*db.RuleHop, ruleMap map[int64]*db.Rule) {
	deltas := make([]counterDelta, 0, len(samples))
	for _, s := range samples {
		var ownerID, ruleID int64
		hopPos := -1
		if hopMap != nil {
			if rh, ok := hopMap[s.proto+"/"+strconv.Itoa(s.port)]; ok {
				if ruleMap != nil {
					if r := ruleMap[rh.RuleID]; r != nil && r.OwnerID.Valid {
						ownerID = r.OwnerID.Int64
					}
				}
				ruleID = rh.RuleID
				hopPos = rh.Position
			}
		}
		deltas = append(deltas, counterDelta{
			proto:         s.proto,
			listenPortStr: strconv.Itoa(s.port),
			bytesUp:       s.sumUp,
			bytesDown:     s.sumDown,
			ownerID:       ownerID,
			ruleID:        ruleID,
			hopPos:        hopPos,
		})
	}
	h.speedCache.update(nodeID, deltas)
}

func (h *Hub) loadCounterSnap(nodeID int64) (*counterSnap, error) {
	epoch := db.DataEpoch()
	now := time.Now()
	h.counterSnaps.mu.Lock()
	if h.counterSnaps.byNode != nil {
		if s := h.counterSnaps.byNode[nodeID]; s != nil && s.epoch == epoch && now.Sub(s.loaded) < counterSnapTTL {
			h.counterSnaps.mu.Unlock()
			return s, nil
		}
	}
	h.counterSnaps.mu.Unlock()

	snap, err := h.fetchCounterSnap(nodeID)
	if err != nil {
		return nil, err
	}
	snap.epoch = epoch
	snap.loaded = now
	if db.DataEpoch() != epoch {
		return snap, nil
	}
	h.counterSnaps.mu.Lock()
	if h.counterSnaps.byNode == nil {
		h.counterSnaps.byNode = map[int64]*counterSnap{}
	}
	h.counterSnaps.byNode[nodeID] = snap
	h.counterSnaps.mu.Unlock()
	return snap, nil
}

func (h *Hub) fetchCounterSnap(nodeID int64) (*counterSnap, error) {
	hopMap, err := db.RuleHopMapByNode(h.DB, nodeID)
	if err != nil {
		return nil, err
	}
	ruleIDSet := map[int64]bool{}
	for _, rh := range hopMap {
		ruleIDSet[rh.RuleID] = true
	}
	ruleIDs := make([]int64, 0, len(ruleIDSet))
	for id := range ruleIDSet {
		ruleIDs = append(ruleIDs, id)
	}
	ruleMap, err := db.RulesByIDs(h.DB, ruleIDs)
	if err != nil {
		return nil, err
	}
	if ruleMap == nil {
		ruleMap = map[int64]*db.Rule{}
	}
	multipliers, err := db.NodeRateMultipliers(h.DB)
	if err != nil {
		return nil, err
	}
	segFirst, err := db.SegmentFirstHops(h.DB, ruleIDs)
	if err != nil {
		return nil, err
	}
	ownerSet := map[int64]bool{}
	for _, r := range ruleMap {
		if r != nil && r.OwnerID.Valid {
			ownerSet[r.OwnerID.Int64] = true
		}
	}
	ownerIDs := make([]int64, 0, len(ownerSet))
	for id := range ownerSet {
		ownerIDs = append(ownerIDs, id)
	}
	exitSet, err := db.PresentLandingExitSet(h.DB, ownerIDs)
	if err != nil {
		return nil, err
	}
	maxPos, err := db.MaxHopPositions(h.DB, ruleIDs)
	if err != nil {
		return nil, err
	}
	node, err := db.GetNode(h.DB, nodeID)
	if err != nil {
		return nil, err
	}
	return &counterSnap{
		hopMap: hopMap, ruleMap: ruleMap, multipliers: multipliers,
		segFirst: segFirst, exitSet: exitSet, maxPos: maxPos,
		unidirectional: node.Unidirectional,
	}, nil
}

// closeForWritePressure drops the agent connection when the panel cannot
// enqueue another frame. Blocking the reader would stall counter ingest and
// apply acks; dropping the frame would let the agent believe a ruleset landed.
func (ac *agentConn) closeForWritePressure() {
	log.Printf("hub: node %d write queue full, closing connection", ac.nodeID)
	ac.signalClose()
	if ac.ws != nil {
		_ = ac.ws.Close(websocket.StatusTryAgainLater, "write queue full")
	}
}
