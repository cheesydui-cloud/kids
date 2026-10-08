package daemon

import (
	"context"
	"errors"
	"testing"

	"nft/internal/forward"
)

// A failed counters send must not lose the deltas: reAddCounters rewinds the
// cursor so the next sample re-reports them, and a successful send (no rollback)
// leaves the cursor advanced so unchanged counters yield nothing.
func TestCounterSamplesRollbackOnFailedSend(t *testing.T) {
	fake := &fakeDataplane{}
	d := newTestDaemon(t)
	d.dp = fake

	fake.counters = []forward.Counter{{Proto: "tcp", ListenPort: 12000, BytesUp: 100, BytesDown: 50}}

	samples := d.counterSamples()
	if len(samples) != 1 || samples[0].BytesUp != 100 || samples[0].BytesDown != 50 {
		t.Fatalf("first sample = %+v, want up=100 down=50", samples)
	}

	// Simulate a failed send: roll the deltas back.
	d.reAddCounters(samples)

	// Kernel counters unchanged → the rolled-back delta must be re-reported.
	samples2 := d.counterSamples()
	if len(samples2) != 1 || samples2[0].BytesUp != 100 || samples2[0].BytesDown != 50 {
		t.Fatalf("after rollback re-report = %+v, want up=100 down=50", samples2)
	}

	// This send "succeeds" (no rollback); with no new bytes the next poll is empty.
	if got := d.counterSamples(); len(got) != 0 {
		t.Fatalf("after commit with no new bytes, want no samples, got %+v", got)
	}
}

// Bytes observed before a successful flush must be reported once on the next
// poll. A flush that zeroes kernel counters, and a reconcile that leaves
// userspace counters untouched, both report the pre-apply delta a single time.
// A failed apply rewinds that sample so the next poll reports it once, not twice.
func TestPreApplySnapshotSurvivesFlush(t *testing.T) {
	fake := &fakeDataplane{}
	d := newTestDaemon(t)
	d.dp = fake

	fake.counters = []forward.Counter{{Proto: "tcp", ListenPort: 12000, BytesUp: 100, BytesDown: 50}}
	fake.afterReconcile = func() {
		fake.counters = []forward.Counter{{Proto: "tcp", ListenPort: 12000, BytesUp: 0, BytesDown: 0}}
	}
	if err := d.applySerialized(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	samples := d.counterSamples()
	if len(samples) != 1 || samples[0].BytesUp != 100 || samples[0].BytesDown != 50 {
		t.Fatalf("after flush = %+v, want up=100 down=50", samples)
	}
	if got := d.counterSamples(); len(got) != 0 {
		t.Fatalf("second poll after flush = %+v, want empty", got)
	}

	fake.counters = []forward.Counter{{Proto: "tcp", ListenPort: 13000, BytesUp: 100, BytesDown: 0}}
	fake.afterReconcile = nil
	if err := d.applySerialized(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	samples = d.counterSamples()
	if len(samples) != 1 || samples[0].ListenPort != 13000 || samples[0].BytesUp != 100 {
		t.Fatalf("userspace-like = %+v, want up=100 once", samples)
	}
	if got := d.counterSamples(); len(got) != 0 {
		t.Fatalf("userspace-like second poll = %+v, want empty", got)
	}

	fake.counters = []forward.Counter{{Proto: "tcp", ListenPort: 14000, BytesUp: 100, BytesDown: 0}}
	fake.err = errors.New("apply failed")
	if err := d.applySerialized(context.Background(), nil); err == nil {
		t.Fatal("expected apply error")
	}
	fake.err = nil
	samples = d.counterSamples()
	if len(samples) != 1 || samples[0].ListenPort != 14000 || samples[0].BytesUp != 100 {
		t.Fatalf("after failed apply = %+v, want up=100 once", samples)
	}
}
