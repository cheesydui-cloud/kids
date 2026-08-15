package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nft/internal/nft"
	"nft/internal/wsproto"
)

func TestDropPanelRulesetClearsPanelKeepsTui(t *testing.T) {
	d := newTestDaemon(t)
	d.owners = OwnerRuleset{
		"panel": {{ID: "p1", Proto: "tcp", SrcPort: 21000, DestIP: "10.0.0.1", DestPort: 443}},
		"tui":   {{ID: "t1", Proto: "tcp", SrcPort: 80, DestIP: "10.0.0.2", DestPort: 80}},
	}
	d.meta.LastAppliedRev = "rev-keep"
	d.lastResolved = append([]nft.Rule(nil), d.owners["panel"]...)
	d.lastResolved = append(d.lastResolved, d.owners["tui"]...)

	d.dropPanelRuleset("test")

	if len(d.owners["panel"]) != 0 {
		t.Fatalf("panel segment still has %d rule(s)", len(d.owners["panel"]))
	}
	if len(d.owners["tui"]) != 1 {
		t.Fatalf("tui segment should stay, got %d", len(d.owners["tui"]))
	}
	if d.meta.LastAppliedRev != "" {
		t.Fatalf("LastAppliedRev should be cleared, got %q", d.meta.LastAppliedRev)
	}
	fake := d.dp.(*fakeDataplane)
	if len(fake.nftCalls) == 0 {
		t.Fatal("expected dataplane reconcile after drop")
	}
	last := fake.nftCalls[len(fake.nftCalls)-1]
	if len(last) != 1 || last[0].ID != "t1" {
		t.Fatalf("dataplane should keep only tui rule, got %+v", last)
	}
}

func TestPanelLeaseLoopDropsAfterExpiry(t *testing.T) {
	d := newTestDaemon(t)
	d.panelLease = 80 * time.Millisecond
	d.owners = OwnerRuleset{
		"panel": {{ID: "p1", Proto: "tcp", SrcPort: 21000, DestIP: "10.0.0.1", DestPort: 443}},
	}
	d.lastResolved = append([]nft.Rule(nil), d.owners["panel"]...)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		d.panelLeaseLoop(ctx)
		close(done)
	}()

	deadline := time.After(1500 * time.Millisecond)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		d.mu.Lock()
		n := len(d.owners["panel"])
		d.mu.Unlock()
		if n == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("panel rules still present after lease")
		case <-tick.C:
		}
	}
	cancel()
	<-done
}

func TestPanelLeaseLoopKeepsRulesWhileConnected(t *testing.T) {
	d := newTestDaemon(t)
	d.panelLease = 80 * time.Millisecond
	d.owners = OwnerRuleset{
		"panel": {{ID: "p1", Proto: "tcp", SrcPort: 21000, DestIP: "10.0.0.1", DestPort: 443}},
	}
	dl := NewDialer(DialerConfig{})
	dl.connected.Store(true)
	d.dialer.Store(dl)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	d.panelLeaseLoop(ctx)

	if len(d.owners["panel"]) != 1 {
		t.Fatalf("connected session must keep panel rules, got %d", len(d.owners["panel"]))
	}
}

func TestDialerHelloRejectedDropsPanel(t *testing.T) {
	fh := newFakeHub()
	fh.onAck(wsproto.TypeHello, func(env wsproto.Envelope) wsproto.Envelope {
		ack, _ := json.Marshal(wsproto.HelloAck{Error: "unknown node token"})
		return wsproto.Envelope{Type: wsproto.TypeHelloAck, ID: env.ID, Payload: ack}
	})
	srv := httptest.NewServer(fh.handler(t))
	defer srv.Close()

	var rejected atomic.Bool
	dl := NewDialer(DialerConfig{
		URL:   "ws" + strings.TrimPrefix(srv.URL, "http") + "/",
		Token: "stale",
		GetState: func() (OwnerRuleset, AgentMeta) {
			return OwnerRuleset{}, AgentMeta{}
		},
		OnHelloRejected: func() { rejected.Store(true) },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	acked, err := dl.runOnce(ctx)
	if acked {
		t.Fatal("hello should not be acked")
	}
	if err == nil || !strings.Contains(err.Error(), "hello rejected") {
		t.Fatalf("want hello rejected, got acked=%v err=%v", acked, err)
	}
	if !rejected.Load() {
		t.Fatal("OnHelloRejected was not called")
	}
}
