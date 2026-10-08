package daemon

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"nft/internal/forward"
	"nft/internal/wsproto"
)

// handleCounters returns per-rule counters merged across the kernel and
// userspace backends. The poller uses these for tenant traffic accounting;
// exposing them on the daemon (not on every client) keeps the data plane as
// the single source of truth.
func (d *Daemon) handleCounters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	counters, err := d.countersFn()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if counters == nil {
		counters = []forward.Counter{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"counters": counters})
}

// counterSamples computes per-rule byte deltas since the last call, for the
// dialer to push to the panel. nft re-applies (flush+recreate) the table on
// every reconcile, zeroing kernel counters, so a current value below the last
// observed one is treated as a reset (delta = current). Deltas sampled just
// before that flush are prepended from pendingCounters; a sample error leaves
// that queue untouched for the next poll.
func (d *Daemon) counterSamples() []wsproto.CounterSample {
	fresh, err := d.sampleDeltas()
	if err != nil {
		log.Printf("counters: %v", err)
		return nil
	}
	d.countersMu.Lock()
	out := append(append([]wsproto.CounterSample{}, d.pendingCounters...), fresh...)
	d.pendingCounters = nil
	d.countersMu.Unlock()
	if len(out) == 0 {
		return nil
	}
	return out
}

// sampleDeltas reads the dataplane, then updates the cursor. The dataplane
// read happens before countersMu so a reconcile holding the dataplane lock
// cannot deadlock against a sampler that already holds countersMu.
func (d *Daemon) sampleDeltas() ([]wsproto.CounterSample, error) {
	cur, err := d.dp.Counters()
	if err != nil {
		return nil, err
	}
	d.countersMu.Lock()
	defer d.countersMu.Unlock()
	if d.lastCounters == nil {
		d.lastCounters = map[string][2]int64{}
	}
	seen := make(map[string]bool, len(cur))
	var out []wsproto.CounterSample
	for _, c := range cur {
		key := c.Proto + "/" + strconv.Itoa(c.ListenPort)
		seen[key] = true
		last := d.lastCounters[key]
		deltaUp := c.BytesUp - last[0]
		if c.BytesUp < last[0] {
			deltaUp = c.BytesUp
		}
		deltaDown := c.BytesDown - last[1]
		if c.BytesDown < last[1] {
			deltaDown = c.BytesDown
		}
		d.lastCounters[key] = [2]int64{c.BytesUp, c.BytesDown}
		if deltaUp > 0 || deltaDown > 0 {
			out = append(out, wsproto.CounterSample{ListenPort: c.ListenPort, Proto: c.Proto, BytesUp: deltaUp, BytesDown: deltaDown})
		}
	}
	for key := range d.lastCounters {
		if !seen[key] {
			delete(d.lastCounters, key)
		}
	}
	return out, nil
}

// reAddCounters rewinds the sampler cursor by the given deltas after a failed
// send so the next counterSamples() call re-reports them. Without this, a
// dropped counters frame silently discards traffic and undercounts quota.
// Bytes that no longer fit in the cursor (the kernel counter was flushed to
// zero after the sample) are put back on pendingCounters instead of dropped.
func (d *Daemon) reAddCounters(samples []wsproto.CounterSample) {
	d.countersMu.Lock()
	defer d.countersMu.Unlock()
	if d.lastCounters == nil {
		d.pendingCounters = append(d.pendingCounters, samples...)
		return
	}
	for _, s := range samples {
		key := s.Proto + "/" + strconv.Itoa(s.ListenPort)
		last, ok := d.lastCounters[key]
		if !ok {
			d.pendingCounters = append(d.pendingCounters, s)
			continue
		}
		up := last[0] - s.BytesUp
		down := last[1] - s.BytesDown
		lostUp, lostDown := int64(0), int64(0)
		// Clamp at 0: a negative cursor would make the next sample's
		// "cur < last" wrap-detection misfire and double-count the rewound
		// bytes if the kernel counter resets (reconcile flush) in between.
		// The clamped remainder is the pre-flush delta, which the kernel no
		// longer holds, so it waits on the pending queue.
		if up < 0 {
			lostUp = -up
			up = 0
		}
		if down < 0 {
			lostDown = -down
			down = 0
		}
		d.lastCounters[key] = [2]int64{up, down}
		if lostUp > 0 || lostDown > 0 {
			d.pendingCounters = append(d.pendingCounters, wsproto.CounterSample{
				ListenPort: s.ListenPort, Proto: s.Proto, BytesUp: lostUp, BytesDown: lostDown,
			})
		}
	}
}
