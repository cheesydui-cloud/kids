package db

import "sync/atomic"

// dataEpoch is a process-local generation for structural panel data: hop
// identity, rule headers, rate multipliers, unidirectional billing, and the
// landing-exit present set. Counter batches cache those lookups and drop the
// cache when the epoch moves. Traffic totals are not structural and must not
// bump this.
var dataEpoch atomic.Int64

// TouchDataEpoch invalidates process-local structural caches. Call it from
// writers that change hop identity, rule headers, node billing factors, or the
// landing-exit present set. A bumped epoch after a rolled-back transaction
// only costs a cache miss.
func TouchDataEpoch() { dataEpoch.Add(1) }

// DataEpoch is the current structural generation. Zero is the initial value.
func DataEpoch() int64 { return dataEpoch.Load() }
