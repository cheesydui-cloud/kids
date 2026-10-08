package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"nft/internal/db"
	"nft/internal/nft"
	"nft/internal/wsproto"
)

const (
	hubWriteTimeout = 10 * time.Second
	hubReadTimeout  = 30 * time.Second
	applyAckTimeout = 60 * time.Second
	// hubMaxReadBytes bounds a single WebSocket frame. Counters samples and
	// rule command payloads are small; anything larger is malformed or malicious.
	hubMaxReadBytes = 4 << 20
	// hubWriteTimeoutMax caps size-based write deadlines (see writeTimeoutFor).
	hubWriteTimeoutMax = 5 * time.Minute
)

// writeTimeoutFor scales the per-frame write deadline with payload size so a
// rare large frame (legacy inline upgrade, big apply payload) is not cut off
// by the default 10s budget on slow reverse links.
func writeTimeoutFor(size int) time.Duration {
	const perMB = 3 * time.Second
	d := hubWriteTimeout + time.Duration(size/(1<<20))*perMB
	if d > hubWriteTimeoutMax {
		return hubWriteTimeoutMax
	}
	return d
}

type Hub struct {
	DB *sql.DB

	// OnTrafficUpdate, when set, is invoked once per (user, node) pair whose
	// usage was advanced by a counters batch. The Hub stays a pure transport:
	// it knows how to accumulate bytes but delegates quota policy (and the
	// re-dispatch it may trigger) to the owner that wires this callback.
	OnTrafficUpdate func(userID int64, nodeID int64)

	// Redispatch re-pushes kernel state to a set of nodes after the hub
	// mutates rule state on their behalf. Keeps the hub transport-only.
	Redispatch func(nodeIDs []int64)

	mu           sync.RWMutex
	conns        map[int64]*agentConn
	speedCache   *speedCache
	counterSnaps counterSnapCache
}

func NewHub(d *sql.DB) *Hub {
	return &Hub{DB: d, conns: make(map[int64]*agentConn), speedCache: newSpeedCache()}
}

type agentConn struct {
	nodeID  int64
	arch    string
	origin  string
	ws      *websocket.Conn
	writeCh chan []byte
	closed  chan struct{}

	// counterWake is a coalescing signal (buffer 1). The reader merges samples
	// into counterPending and returns; counterLoop does the database work.
	counterWake    chan struct{}
	counterMu      sync.Mutex
	counterPending []accountedSample
	counterIndex   map[string]int
	// lastSeenUnix is the last TouchNodeLastSeen attempt, in Unix nanoseconds.
	lastSeenUnix atomic.Int64

	// closeOnce guards closed so the multiple close paths (a displaced
	// conn in registerConn, unregisterConn on disconnect, and Hub.Close
	// on shutdown) can race without double-closing the channel.
	closeOnce sync.Once

	pendMu  sync.Mutex
	pending map[string]chan json.RawMessage

	idSeq atomic.Uint64
}

func (a *agentConn) nextID() string {
	return strconv.FormatUint(a.idSeq.Add(1), 36)
}

// signalClose closes ac.closed exactly once, signalling the reader and
// writer loops (and any pending SendApplyRuleset) to stop.
func (a *agentConn) signalClose() {
	a.closeOnce.Do(func() { close(a.closed) })
}

func (h *Hub) IsOnline(nodeID int64) bool {
	h.mu.RLock()
	_, ok := h.conns[nodeID]
	h.mu.RUnlock()
	return ok
}

func (h *Hub) NodeArch(nodeID int64) string {
	h.mu.RLock()
	ac, ok := h.conns[nodeID]
	h.mu.RUnlock()
	if !ok || ac == nil {
		return ""
	}
	return ac.arch
}

// NodeOrigin is scheme://host the agent used to reach this panel (after
// X-Forwarded-*). Empty when the node is offline. Upgrade downloads should
// use this instead of settings.panel_url so a domestic node that connected
// via a CN domain is not sent an overseas IP it cannot pull 8MB from.
func (h *Hub) NodeOrigin(nodeID int64) string {
	h.mu.RLock()
	ac, ok := h.conns[nodeID]
	h.mu.RUnlock()
	if !ok || ac == nil {
		return ""
	}
	return ac.origin
}

// ServeWS handles the /v1/agents WS endpoint. Upgrades the request,
// reads the mandatory hello frame, validates the bearer token against
// nodes.secret, registers the conn, and loops on reads dispatching by
// message type.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // we authenticate via bearer in hello
	})
	if err != nil {
		log.Printf("hub: accept: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	helloEnv, err := readEnvelope(ctx, ws, hubReadTimeout)
	if err != nil || helloEnv.Type != wsproto.TypeHello {
		writeError(ctx, ws, "protocol", "expected hello as first frame")
		ws.Close(websocket.StatusPolicyViolation, "no hello")
		return
	}
	var hello wsproto.Hello
	if err := json.Unmarshal(helloEnv.Payload, &hello); err != nil {
		writeError(ctx, ws, "protocol", "malformed hello payload")
		ws.Close(websocket.StatusPolicyViolation, "bad hello")
		return
	}

	node, err := lookupNodeBySecret(h.DB, hello.NodeToken)
	if err != nil || node == nil {
		ack, _ := json.Marshal(wsproto.HelloAck{Error: "unknown or revoked token"})
		writeEnvelope(ctx, ws, wsproto.Envelope{Type: wsproto.TypeHelloAck, ID: helloEnv.ID, Payload: ack})
		ws.Close(websocket.StatusPolicyViolation, "bad token")
		return
	}

	// Register before hello_ack so both sides can rely on the invariant
	// "hello_ack visible => conn is in the hub map".
	ac := &agentConn{
		nodeID:      node.ID,
		arch:        hello.Arch,
		origin:      requestOrigin(r),
		ws:          ws,
		writeCh:     make(chan []byte, 16),
		closed:      make(chan struct{}),
		pending:     make(map[string]chan json.RawMessage),
		counterWake: make(chan struct{}, 1),
	}
	ac.lastSeenUnix.Store(time.Now().UnixNano())
	h.registerConn(ac)
	defer h.unregisterConn(ac)

	poolSize := 4
	if psStr, err := db.GetSetting(h.DB, "pool_size"); err == nil {
		if n, err := strconv.Atoi(psStr); err == nil && n >= 0 {
			poolSize = n
		}
	}
	ackPayload, _ := json.Marshal(wsproto.HelloAck{
		NodeID:            node.ID,
		Name:              node.Name,
		PoolSize:          poolSize,
		PanelLeaseSeconds: panelLeaseHoursFromDB(h.DB) * 3600,
	})
	if err := writeEnvelope(ctx, ws, wsproto.Envelope{Type: wsproto.TypeHelloAck, ID: helloEnv.ID, Payload: ackPayload}); err != nil {
		ws.Close(websocket.StatusInternalError, "ack write failed")
		return
	}

	connectIP := extractIP(r)
	if err := db.MarkNodeOnline(h.DB, node.ID, hello.AgentVersion, hello.AgentSHA, connectIP); err != nil {
		log.Printf("hub: MarkNodeOnline: %v", err)
	}
	applyDeclaredRelayHosts(h.DB, node, hello.DeclaredRelayHost, hello.DeclaredRelayHostV6)
	fillNodeRelayHosts(h.DB, node, connectIP, hello.ProbedV4, hello.ProbedV6, hello.DeclaredRelayHost, hello.DeclaredRelayHostV6)
	if hello.PortRange != "" {
		if err := db.ValidatePortRange(hello.PortRange); err == nil {
			_ = db.UpdateNodePortRange(h.DB, node.ID, hello.PortRange)
		}
	}

	// A node may have missed rule changes while it was offline. Reconcile in the
	// background so the hello path is not stuck on the ruleset query. writerLoop
	// is already running, and readerLoop (below) can accept the apply_ack.
	go h.writerLoop(ac)
	go h.counterLoop(ac)
	go h.reconcileOnConnect(node.ID, hello.LastAppliedRev)
	h.readerLoop(ctx, ac)
}

// reconcileOnConnect re-pushes the node's ruleset after a (re)connect unless the
// agent's reported last-applied rev already matches what the panel would send.
// Redispatch runs off-goroutine, so it schedules the apply without blocking the
// hello path; the apply_ack is handled once readerLoop starts.
func (h *Hub) reconcileOnConnect(nodeID int64, lastAppliedRev string) {
	if h.Redispatch == nil {
		return
	}
	ruleHops, err := db.ActiveRuleHopsForPush(h.DB, nodeID)
	if err != nil {
		// Can't compute the target rev — force a resync rather than risk drift.
		go h.Redispatch([]int64{nodeID})
		return
	}
	rules, err := buildRulesChecked(h.DB, ruleHops)
	if err != nil {
		// Can't compute the target rev — force a resync rather than risk drift.
		go h.Redispatch([]int64{nodeID})
		return
	}
	if lastAppliedRev != "" && computeRev(rules) == lastAppliedRev {
		return
	}
	go h.Redispatch([]int64{nodeID})
}

func (h *Hub) registerConn(ac *agentConn) {
	h.mu.Lock()
	if old, ok := h.conns[ac.nodeID]; ok {
		old.signalClose()
		old.ws.Close(websocket.StatusGoingAway, "replaced by newer connection")
	}
	h.conns[ac.nodeID] = ac
	h.mu.Unlock()
}

func (h *Hub) unregisterConn(ac *agentConn) {
	h.mu.Lock()
	if cur, ok := h.conns[ac.nodeID]; ok && cur == ac {
		delete(h.conns, ac.nodeID)
	}
	h.mu.Unlock()
	ac.signalClose()
	_ = db.MarkNodeOffline(h.DB, ac.nodeID)
}

// Close gracefully shuts down every agent connection.
func (h *Hub) Close() {
	h.mu.Lock()
	conns := make([]*agentConn, 0, len(h.conns))
	for _, ac := range h.conns {
		conns = append(conns, ac)
	}
	h.conns = make(map[int64]*agentConn)
	h.mu.Unlock()

	for _, ac := range conns {
		ac.signalClose()
		_ = ac.ws.Close(websocket.StatusGoingAway, "panel shutting down")
	}
}

func (h *Hub) writerLoop(ac *agentConn) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hub: writerLoop panic for node %d: %v", ac.nodeID, r)
			ac.signalClose()
		}
	}()
	for {
		select {
		case <-ac.closed:
			return
		case b := <-ac.writeCh:
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeoutFor(len(b)))
			err := ac.ws.Write(ctx, websocket.MessageText, b)
			cancel()
			if err != nil {
				ac.ws.Close(websocket.StatusInternalError, "write error")
				return
			}
		}
	}
}

func (h *Hub) readerLoop(parent context.Context, ac *agentConn) {
	defer func() {
		// The counter worker can observe close and exit before this loop
		// finishes the frame it already read. Flush here so that batch is
		// still recorded.
		h.flushPendingCounters(ac)
		if r := recover(); r != nil {
			log.Printf("hub: readerLoop panic for node %d: %v", ac.nodeID, r)
			ac.signalClose()
		}
	}()
	for {
		ctx, cancel := context.WithTimeout(parent, hubReadTimeout)
		_, b, err := ac.ws.Read(ctx)
		cancel()
		if err != nil {
			return
		}
		var env wsproto.Envelope
		if err := json.Unmarshal(b, &env); err != nil {
			log.Printf("hub: malformed envelope from node %d: %v", ac.nodeID, err)
			continue
		}
		switch env.Type {
		case wsproto.TypePing:
			pong, _ := json.Marshal(wsproto.Pong{TS: time.Now().UnixMilli()})
			ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypePong, ID: env.ID, Payload: pong})
			// Heartbeat writes are throttled. Agents ping ~10s; one UPDATE
			// every 20s still lands inside the 45s stale-online window.
			ac.maybeTouchLastSeen(h.DB)
		case wsproto.TypeCounters:
			var co wsproto.Counters
			if err := json.Unmarshal(env.Payload, &co); err != nil {
				log.Printf("hub: node %d malformed counters: %v", ac.nodeID, err)
				continue
			}
			ac.enqueueCounters(co.Samples)
		case wsproto.TypeRuleCreate:
			h.handleRuleCreate(ac, env)
		case wsproto.TypeRuleUpdate:
			h.handleRuleUpdate(ac, env)
		case wsproto.TypeMigrateRules:
			h.handleMigrateRules(ac, env)
		case wsproto.TypeRuleHopEdit:
			var e wsproto.RuleHopEdit
			if err := json.Unmarshal(env.Payload, &e); err != nil {
				sendRuleAckErr(ac, env.ID, "malformed payload")
				continue
			}
			entry, cerr := h.applyRuleHopEdit(ac.nodeID, e.RuleID, e.ListenPort, e.Mode, e.Comment)
			ack := wsproto.RuleCmdAck{OK: cerr == nil, Entry: entry}
			if cerr != nil {
				ack.Error = cerr.Error()
			}
			ackP, _ := json.Marshal(ack)
			ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeRuleCmdAck, ID: env.ID, Payload: ackP})
		case wsproto.TypeRuleDelete:
			var dl wsproto.RuleDelete
			if err := json.Unmarshal(env.Payload, &dl); err != nil {
				sendRuleAckErr(ac, env.ID, "malformed payload")
				continue
			}
			cerr := h.applyRuleDelete(ac.nodeID, dl.RuleID)
			ack := wsproto.RuleCmdAck{OK: cerr == nil}
			if cerr != nil {
				ack.Error = cerr.Error()
			}
			ackP, _ := json.Marshal(ack)
			ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeRuleCmdAck, ID: env.ID, Payload: ackP})
		case wsproto.TypeApplyAck, wsproto.TypeHelloAck, wsproto.TypeUpgradeAck, wsproto.TypeProbeAck:
			ac.dispatchAck(env)
		default:
			log.Printf("hub: node %d unknown frame type %q", ac.nodeID, env.Type)
		}
	}
}

func (ac *agentConn) enqueueWrite(env wsproto.Envelope) {
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	select {
	case ac.writeCh <- b:
	case <-ac.closed:
	default:
		ac.closeForWritePressure()
	}
}

func (ac *agentConn) dispatchAck(env wsproto.Envelope) {
	ac.pendMu.Lock()
	ch, ok := ac.pending[env.ID]
	if ok {
		delete(ac.pending, env.ID)
	}
	ac.pendMu.Unlock()
	if ok {
		ch <- env.Payload
	}
}

func (h *Hub) SendApplyRuleset(nodeID int64, rules []nft.Rule, rev string) (string, error) {
	h.mu.RLock()
	ac, ok := h.conns[nodeID]
	h.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("node %d not connected", nodeID)
	}
	id := ac.nextID()
	ch := make(chan json.RawMessage, 1)
	ac.pendMu.Lock()
	ac.pending[id] = ch
	ac.pendMu.Unlock()
	defer func() {
		ac.pendMu.Lock()
		delete(ac.pending, id)
		ac.pendMu.Unlock()
	}()

	payload, _ := json.Marshal(wsproto.ApplyRuleset{Rev: rev, Rules: rules})
	ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeApplyRuleset, ID: id, Payload: payload})

	select {
	case raw := <-ch:
		var ack wsproto.ApplyAck
		if err := json.Unmarshal(raw, &ack); err != nil {
			return "", fmt.Errorf("malformed apply_ack: %w", err)
		}
		if !ack.OK {
			return "", fmt.Errorf("apply rejected: %s", ack.Error)
		}
		return ack.Warning, nil
	case <-time.After(applyAckTimeout):
		return "", errors.New("apply_ack timeout")
	case <-ac.closed:
		return "", errors.New("connection closed before ack")
	}
}

func (h *Hub) SendProbe(nodeID int64, target string) (wsproto.ProbeAck, error) {
	return h.SendProbeMode(nodeID, target, "")
}

func (h *Hub) SendProbeMode(nodeID int64, target, mode string) (wsproto.ProbeAck, error) {
	h.mu.RLock()
	ac, ok := h.conns[nodeID]
	h.mu.RUnlock()
	if !ok {
		return wsproto.ProbeAck{}, fmt.Errorf("node %d not connected", nodeID)
	}
	id := ac.nextID()
	ch := make(chan json.RawMessage, 1)
	ac.pendMu.Lock()
	ac.pending[id] = ch
	ac.pendMu.Unlock()
	defer func() {
		ac.pendMu.Lock()
		delete(ac.pending, id)
		ac.pendMu.Unlock()
	}()

	payload, _ := json.Marshal(wsproto.Probe{Target: target, Mode: mode})
	ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeProbe, ID: id, Payload: payload})

	select {
	case raw := <-ch:
		var ack wsproto.ProbeAck
		if err := json.Unmarshal(raw, &ack); err != nil {
			return wsproto.ProbeAck{}, fmt.Errorf("malformed probe_ack: %w", err)
		}
		return ack, nil
	case <-time.After(10 * time.Second):
		return wsproto.ProbeAck{}, errors.New("probe timeout")
	case <-ac.closed:
		return wsproto.ProbeAck{}, errors.New("connection closed")
	}
}

func (h *Hub) BroadcastConfigUpdate(poolSize, leaseSeconds int) {
	payload, _ := json.Marshal(wsproto.ConfigUpdate{PoolSize: poolSize, PanelLeaseSeconds: leaseSeconds})
	env := wsproto.Envelope{Type: wsproto.TypeConfigUpdate, Payload: payload}
	h.mu.RLock()
	conns := make([]*agentConn, 0, len(h.conns))
	for _, ac := range h.conns {
		conns = append(conns, ac)
	}
	h.mu.RUnlock()
	for _, ac := range conns {
		ac.enqueueWrite(env)
	}
}

// Helpers --------------------------------------------------------------

// trustedProxyNets contains the CIDRs from which X-Forwarded-For / X-Real-IP
// are considered trustworthy. Defaults to loopback and RFC1918 private ranges
// so a same-machine or LAN reverse proxy is trusted, but a random internet peer
// cannot spoof its address. The server also calls this for the speed/landing
// HTTP endpoints, so it lives on Hub's helper section rather than inside the
// WS handler.
var trustedProxyNets = func() []*net.IPNet {
	nets := []*net.IPNet{}
	for _, cidr := range []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func isTrustedProxy(ip net.IP) bool {
	for _, n := range trustedProxyNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func extractIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remoteIP := net.ParseIP(host)
	if remoteIP != nil && isTrustedProxy(remoteIP) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.Index(xff, ","); i > 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}

// fillNodeRelayHosts seeds relay_host/relay_host_v6 for a node that hasn't
// had them set yet. connectIP (the address the panel observed this WS
// connection arrive from) is authoritative for whichever family it belongs
// to — it reflects the address as seen after any NAT, unlike a locally
// self-probed address. The agent's self-probed address only fills the
// OTHER family, the one this connection didn't use. Never overwrites a
// manually-configured value (only fires when the DB field is still empty).
//
// relay_host must always hold a v4 literal or hostname: an IPv6 literal
// found there can only be leftover data from before the two fields were
// split by address family. Such a value is evicted from relay_host
// unconditionally, so the empty-field seeding below can re-fill it with a
// proper v4 value. It is migrated into relay_host_v6 only when this
// connection didn't itself supply a fresher v6 connectIP: connectIP is
// always more authoritative than data carried over from before the split,
// since the agent's address may well have changed since that data was
// written. This makes stale data self-heal on the node's next connection
// instead of persisting forever, without letting the stale value block a
// fresher one from landing.
//
// declaredV4/declaredV6 come from this same hello's applyDeclaredRelayHosts
// call. A non-empty declared value — even one that failed validation and so
// was never written — means the operator expressed intent for that family;
// auto-filling it from connectIP would silently paper over a rejected
// declaration with the very kind of address (the connection's outbound
// route) the declare feature exists to override. So a non-empty declared
// value suppresses auto-fill for its family regardless of whether it was
// valid. Nodes that never declare anything see declaredV4/declaredV6 always
// empty, so this guard is always true for them and behavior is unchanged.
func fillNodeRelayHosts(d *sql.DB, node *db.Node, connectIP, probedV4, probedV6, declaredV4, declaredV6 string) {
	connectIsV6 := false
	if ip := net.ParseIP(connectIP); ip != nil {
		connectIsV6 = ip.To4() == nil
	}
	if ip := net.ParseIP(node.RelayHost); ip != nil && ip.To4() == nil {
		if node.RelayHostV6 == "" && !(connectIsV6 && connectIP != "") {
			_ = db.UpdateNodeRelayHostV6(d, node.ID, node.RelayHost)
			node.RelayHostV6 = node.RelayHost
		}
		_ = db.UpdateNodeRelayHost(d, node.ID, "")
		node.RelayHost = ""
	}
	if node.RelayHost == "" && declaredV4 == "" {
		if !connectIsV6 && connectIP != "" {
			_ = db.UpdateNodeRelayHost(d, node.ID, connectIP)
		} else if probedV4 != "" {
			_ = db.UpdateNodeRelayHost(d, node.ID, probedV4)
		}
	}
	if node.RelayHostV6 == "" && declaredV6 == "" {
		if connectIsV6 && connectIP != "" {
			_ = db.UpdateNodeRelayHostV6(d, node.ID, connectIP)
		} else if probedV6 != "" {
			_ = db.UpdateNodeRelayHostV6(d, node.ID, probedV6)
		}
	}
}

// applyDeclaredRelayHosts handles operator-declared relay_host/relay_host_v6
// values sent via Hello.DeclaredRelayHost/DeclaredRelayHostV6 (see
// cmd/nft-agent's --relay-host/--relay-host-v6 flags). Unlike
// fillNodeRelayHosts, which only ever seeds an empty field once, a declared
// value is authoritative: it overwrites whatever is in the DB on every
// hello where it's present, so config drift self-heals. When the daemon
// stops declaring a value (flag removed, daemon restarted), the DB field
// unlocks but keeps its last value rather than going blank, so a live route
// doesn't disappear out from under the running link.
func applyDeclaredRelayHosts(d *sql.DB, node *db.Node, declaredV4, declaredV6 string) {
	if declaredV4 != "" {
		if isValidRelayHost(declaredV4) {
			if node.RelayHost != declaredV4 || !node.RelayHostDeclared {
				_ = db.UpdateNodeRelayHost(d, node.ID, declaredV4)
				_ = db.SetNodeRelayHostDeclared(d, node.ID, true)
				node.RelayHost, node.RelayHostDeclared = declaredV4, true
			}
		} else {
			log.Printf("hub: node %d declared invalid relay_host %q, ignoring", node.ID, declaredV4)
		}
	} else if node.RelayHostDeclared {
		_ = db.SetNodeRelayHostDeclared(d, node.ID, false)
		node.RelayHostDeclared = false
	}

	if declaredV6 != "" {
		if isValidRelayHostV6(declaredV6) {
			if node.RelayHostV6 != declaredV6 || !node.RelayHostV6Declared {
				_ = db.UpdateNodeRelayHostV6(d, node.ID, declaredV6)
				_ = db.SetNodeRelayHostV6Declared(d, node.ID, true)
				node.RelayHostV6, node.RelayHostV6Declared = declaredV6, true
			}
		} else {
			log.Printf("hub: node %d declared invalid relay_host_v6 %q, ignoring", node.ID, declaredV6)
		}
	} else if node.RelayHostV6Declared {
		_ = db.SetNodeRelayHostV6Declared(d, node.ID, false)
		node.RelayHostV6Declared = false
	}
}

func lookupNodeBySecret(d *sql.DB, secret string) (*db.Node, error) {
	if secret == "" {
		return nil, errors.New("empty secret")
	}
	// Node secrets are stored in plaintext; the agent presents the same value,
	// so compare directly. (Legacy v3.0.0 rows stored a SHA-256 hash and will
	// no longer match — those nodes must reset their token to reconnect.)
	var id int64
	err := d.QueryRow(`SELECT id FROM nodes WHERE secret=? AND disabled=0`, secret).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return db.GetNode(d, id)
}

func readEnvelope(ctx context.Context, ws *websocket.Conn, timeout time.Duration) (wsproto.Envelope, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, r, err := ws.Reader(rctx)
	if err != nil {
		return wsproto.Envelope{}, err
	}
	b, err := io.ReadAll(io.LimitReader(r, hubMaxReadBytes+1))
	if err != nil {
		return wsproto.Envelope{}, err
	}
	if len(b) > hubMaxReadBytes {
		return wsproto.Envelope{}, fmt.Errorf("frame exceeds %d bytes", hubMaxReadBytes)
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return wsproto.Envelope{}, err
	}
	return env, nil
}

func writeEnvelope(ctx context.Context, ws *websocket.Conn, env wsproto.Envelope) error {
	b, err := json.Marshal(env)
	if err != nil {
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeoutFor(len(b)))
	defer cancel()
	return ws.Write(wctx, websocket.MessageText, b)
}

func writeError(ctx context.Context, ws *websocket.Conn, code, msg string) {
	p, _ := json.Marshal(wsproto.Error{Code: code, Message: msg})
	_ = writeEnvelope(ctx, ws, wsproto.Envelope{Type: wsproto.TypeError, Payload: p})
}

// applyRuleHopEdit folds a node-reported edit to its hop in ruleID back
// into the rule skeleton and re-dispatches every node the regeneration
// touched, returning the rule's copyable entry endpoint. The hop is located
// by (ruleID, nodeID): a rule can't repeat a node, so that pair is unique.
func (h *Hub) applyRuleHopEdit(nodeID, ruleID int64, listenPort int, mode, comment string) (string, error) {
	r, err := db.GetRule(h.DB, ruleID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("规则不存在")
	}
	if err != nil {
		return "", err
	}
	hops, err := db.ListRuleHops(h.DB, ruleID)
	if err != nil {
		return "", err
	}
	found := false
	inputs := make([]db.HopInput, len(hops))
	for i, hp := range hops {
		in := db.HopInput{NodeID: hp.NodeID, Mode: hp.Mode, ViaNodeID: hp.ViaNodeID}
		if hp.NodeID == nodeID {
			found = true
			in.DesiredPort = listenPort
			in.Mode = db.NormalizeForwardMode(mode)
			in.Comment = comment
		}
		inputs[i] = in
	}
	if !found {
		return "", fmt.Errorf("节点不在该规则上")
	}
	tx, err := h.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	entry, _, affected, err := db.RegenerateRule(tx, r, inputs, nil)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if h.Redispatch != nil {
		h.Redispatch(affected)
	}
	return entry, nil
}

// applyRuleDelete removes the whole rule (all hops on all nodes) on behalf
// of a node that participates in it, then re-dispatches every node that ran
// its hops so the deleted rules leave the kernel.
func (h *Hub) applyRuleDelete(nodeID, ruleID int64) error {
	hops, err := db.ListRuleHops(h.DB, ruleID)
	if err != nil {
		return err
	}
	onRule := false
	for _, hp := range hops {
		if hp.NodeID == nodeID {
			onRule = true
			break
		}
	}
	if !onRule {
		return fmt.Errorf("节点不在该规则上")
	}
	nodes, err := db.DeleteRule(h.DB, ruleID)
	if err != nil {
		return err
	}
	if h.Redispatch != nil {
		h.Redispatch(nodes)
	}
	return nil
}

// handleRuleCreate creates a new single-hop rule on the requesting node.
// The node must have an owner (via node.owner_id) who is active and within quota.
func (h *Hub) handleRuleCreate(ac *agentConn, env wsproto.Envelope) {
	var req wsproto.RuleCreate
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		sendRuleAckErr(ac, env.ID, "malformed payload")
		return
	}
	node, err := db.GetNode(h.DB, ac.nodeID)
	if err != nil {
		sendRuleAckErr(ac, env.ID, "节点不存在")
		return
	}
	if node.OwnerID == nil {
		sendRuleAckErr(ac, env.ID, "节点无归属用户")
		return
	}
	ownerID := *node.OwnerID
	u, err := db.GetUserByID(h.DB, ownerID)
	if err != nil {
		sendRuleAckErr(ac, env.ID, "归属用户不存在")
		return
	}
	if u.Disabled {
		sendRuleAckErr(ac, env.ID, "用户已被禁用")
		return
	}
	if u.ExpiresAt.Valid && u.ExpiresAt.Int64 > 0 && u.ExpiresAt.Int64 < time.Now().Unix() {
		sendRuleAckErr(ac, env.ID, "用户已过期")
		return
	}
	total, _ := db.CountRulesForUser(h.DB, ownerID)
	if total >= u.MaxForwards {
		sendRuleAckErr(ac, env.ID, fmt.Sprintf("超出用户最大转发数（%d）", u.MaxForwards))
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		sendRuleAckErr(ac, env.ID, "内部错误")
		return
	}
	defer tx.Rollback()

	rl := &db.Rule{
		NodeID:   ac.nodeID,
		OwnerID:  sql.NullInt64{Int64: ownerID, Valid: true},
		Name:     req.Name,
		Proto:    req.Proto,
		ExitHost: req.ExitHost,
		ExitPort: req.ExitPort,
		Comment:  req.Comment,
	}
	id, err := db.CreateRule(tx, rl)
	if err != nil {
		sendRuleAckErr(ac, env.ID, err.Error())
		return
	}
	rl.ID = id

	hop := db.HopInput{NodeID: ac.nodeID, Mode: db.NormalizeForwardMode(req.Mode), ViaNodeID: ac.nodeID}
	if req.ListenPort > 0 {
		hop.DesiredPort = req.ListenPort
	}
	entry, _, affected, err := db.RegenerateRule(tx, rl, []db.HopInput{hop}, nil)
	if err != nil {
		sendRuleAckErr(ac, env.ID, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		sendRuleAckErr(ac, env.ID, "提交失败")
		return
	}
	if h.Redispatch != nil {
		go h.Redispatch(affected)
	}
	ackP, _ := json.Marshal(wsproto.RuleCmdAck{OK: true, Entry: entry})
	ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeRuleCmdAck, ID: env.ID, Payload: ackP})
}

// handleRuleUpdate modifies a single-hop rule's header and regenerates it.
// Only rules with exactly 1 hop that belong to the node's owner are editable.
func (h *Hub) handleRuleUpdate(ac *agentConn, env wsproto.Envelope) {
	var req wsproto.RuleUpdate
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		sendRuleAckErr(ac, env.ID, "malformed payload")
		return
	}
	node, err := db.GetNode(h.DB, ac.nodeID)
	if err != nil {
		sendRuleAckErr(ac, env.ID, "节点不存在")
		return
	}
	if node.OwnerID == nil {
		sendRuleAckErr(ac, env.ID, "节点无归属用户")
		return
	}
	rl, err := db.GetRule(h.DB, req.RuleID)
	if err != nil {
		sendRuleAckErr(ac, env.ID, "规则不存在")
		return
	}
	if !rl.OwnerID.Valid || rl.OwnerID.Int64 != *node.OwnerID {
		sendRuleAckErr(ac, env.ID, "无权操作该规则")
		return
	}
	// Only single-hop rules are editable from a node
	hops, err := db.ListRuleHops(h.DB, req.RuleID)
	if err != nil {
		sendRuleAckErr(ac, env.ID, "读取跳数失败")
		return
	}
	if len(hops) != 1 {
		sendRuleAckErr(ac, env.ID, "仅支持编辑单跳规则")
		return
	}

	rl.Name = req.Name
	rl.Proto = req.Proto
	rl.ExitHost = req.ExitHost
	rl.ExitPort = req.ExitPort
	rl.Comment = req.Comment

	tx, err := h.DB.Begin()
	if err != nil {
		sendRuleAckErr(ac, env.ID, "内部错误")
		return
	}
	defer tx.Rollback()

	if err := db.UpdateRuleHeader(tx, rl); err != nil {
		sendRuleAckErr(ac, env.ID, err.Error())
		return
	}
	hop := db.HopInput{NodeID: ac.nodeID, Mode: db.NormalizeForwardMode(req.Mode), ViaNodeID: ac.nodeID}
	if req.ListenPort > 0 {
		hop.DesiredPort = req.ListenPort
	}
	entry, _, affected, err := db.RegenerateRule(tx, rl, []db.HopInput{hop}, nil)
	if err != nil {
		sendRuleAckErr(ac, env.ID, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		sendRuleAckErr(ac, env.ID, "提交失败")
		return
	}
	if h.Redispatch != nil {
		go h.Redispatch(affected)
	}
	ackP, _ := json.Marshal(wsproto.RuleCmdAck{OK: true, Entry: entry})
	ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeRuleCmdAck, ID: env.ID, Payload: ackP})
}

// handleMigrateRules bulk-imports rules from an agent's local state.
// Each rule becomes a new single-hop rule owned by the node's owner.
func (h *Hub) handleMigrateRules(ac *agentConn, env wsproto.Envelope) {
	var req wsproto.MigrateRules
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		sendRuleAckErr(ac, env.ID, "malformed payload")
		return
	}
	node, err := db.GetNode(h.DB, ac.nodeID)
	if err != nil {
		sendRuleAckErr(ac, env.ID, "节点不存在")
		return
	}
	if node.OwnerID == nil {
		sendRuleAckErr(ac, env.ID, "节点无归属用户")
		return
	}
	ownerID := *node.OwnerID
	u, err := db.GetUserByID(h.DB, ownerID)
	if err != nil {
		sendRuleAckErr(ac, env.ID, "归属用户不存在")
		return
	}
	if u.Disabled {
		sendRuleAckErr(ac, env.ID, "用户已被禁用")
		return
	}
	if u.ExpiresAt.Valid && u.ExpiresAt.Int64 > 0 && u.ExpiresAt.Int64 < time.Now().Unix() {
		sendRuleAckErr(ac, env.ID, "用户已过期")
		return
	}

	total, _ := db.CountRulesForUser(h.DB, ownerID)
	if total+len(req.Rules) > u.MaxForwards {
		sendRuleAckErr(ac, env.ID, fmt.Sprintf("超出用户最大转发数（%d），当前 %d 条，迁入 %d 条", u.MaxForwards, total, len(req.Rules)))
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		sendRuleAckErr(ac, env.ID, "内部错误")
		return
	}
	defer tx.Rollback()

	var allAffected []int64
	for _, r := range req.Rules {
		exitHost := r.DestIP
		if r.DestHost != "" {
			exitHost = r.DestHost
		}
		name := r.Comment
		if name == "" {
			name = r.RuleName
		}
		rl := &db.Rule{
			NodeID:   ac.nodeID,
			OwnerID:  sql.NullInt64{Int64: ownerID, Valid: true},
			Name:     name,
			Proto:    r.Proto,
			ExitHost: exitHost,
			ExitPort: r.DestPort,
		}
		id, err := db.CreateRule(tx, rl)
		if err != nil {
			sendRuleAckErr(ac, env.ID, fmt.Sprintf("创建规则失败: %v", err))
			return
		}
		rl.ID = id
		hop := db.HopInput{NodeID: ac.nodeID, Mode: db.NormalizeForwardMode(r.Mode), ViaNodeID: ac.nodeID}
		if r.SrcPort > 0 {
			hop.DesiredPort = r.SrcPort
		}
		_, _, affected, err := db.RegenerateRule(tx, rl, []db.HopInput{hop}, nil)
		if err != nil {
			sendRuleAckErr(ac, env.ID, fmt.Sprintf("生成规则失败: %v", err))
			return
		}
		allAffected = append(allAffected, affected...)
	}

	if err := tx.Commit(); err != nil {
		sendRuleAckErr(ac, env.ID, "提交失败")
		return
	}
	if h.Redispatch != nil {
		go h.Redispatch(allAffected)
	}
	ackP, _ := json.Marshal(wsproto.RuleCmdAck{OK: true})
	ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeRuleCmdAck, ID: env.ID, Payload: ackP})
}

func sendRuleAckErr(ac *agentConn, id, msg string) {
	ackP, _ := json.Marshal(wsproto.RuleCmdAck{OK: false, Error: msg})
	ac.enqueueWrite(wsproto.Envelope{Type: wsproto.TypeRuleCmdAck, ID: id, Payload: ackP})
}
