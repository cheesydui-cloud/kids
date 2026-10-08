package db

import (
	"fmt"
	"strings"
	"time"
)

// counterBatchChunk is how many rows one UPDATE ... FROM statement binds.
// SQLite's variable limit is far higher; 80 keeps statements small and still
// collapses a counters batch into a handful of round-trips.
var counterBatchChunk = 80

// RuleHopCounterUpdate is one rule_hops row accumulated from a counters batch.
// Last* is the most recent sample; Add* is the sum, which can be larger when
// several samples were coalesced.
type RuleHopCounterUpdate struct {
	ID                          int64
	LastBytes, LastUp, LastDown int64
	AddTotal, AddBilled         int64
}

// UserNodeTrafficUpdate adds raw bytes to one user_nodes grant.
type UserNodeTrafficUpdate struct {
	UserID, NodeID, Delta int64
}

// UserTrafficUpdate adds the same final-hop raw bytes to the cycle counter,
// the lifetime counter, and today's per-user ledger.
type UserTrafficUpdate struct {
	UserID       int64
	Cycle, Total int64
}

// IDDelta adds delta bytes to a row keyed by a single id.
type IDDelta struct {
	ID, Delta int64
}

// LandingExitTrafficUpdate adds raw bytes to one present landing-exit ledger.
type LandingExitTrafficUpdate struct {
	UserID int64
	Host   string
	Port   int
	Delta  int64
}

// UpdateRuleHopCounters writes hop cursors and totals in chunked UPDATE FROM
// statements. An empty slice is a no-op.
func UpdateRuleHopCounters(tx DBTX, rows []RuleHopCounterUpdate) error {
	vals := make([][]any, len(rows))
	for i, r := range rows {
		vals[i] = []any{r.ID, r.LastBytes, r.LastUp, r.LastDown, r.AddTotal, r.AddBilled}
	}
	return execUpdateFrom(tx, `UPDATE rule_hops SET
		last_bytes = v.last_bytes,
		last_bytes_up = v.last_up,
		last_bytes_down = v.last_down,
		total_bytes = rule_hops.total_bytes + v.add_total,
		billed_bytes = rule_hops.billed_bytes + v.add_billed`,
		[]string{"id", "last_bytes", "last_up", "last_down", "add_total", "add_billed"},
		"rule_hops.id = v.id", vals)
}

// UpdateUserNodeTrafficBatch adds per-grant usage in chunked UPDATE FROM
// statements.
func UpdateUserNodeTrafficBatch(tx DBTX, rows []UserNodeTrafficUpdate) error {
	vals := make([][]any, len(rows))
	for i, r := range rows {
		vals[i] = []any{r.UserID, r.NodeID, r.Delta}
	}
	return execUpdateFrom(tx, `UPDATE user_nodes SET
		traffic_used_bytes = user_nodes.traffic_used_bytes + v.delta`,
		[]string{"user_id", "node_id", "delta"},
		"user_nodes.user_id = v.user_id AND user_nodes.node_id = v.node_id", vals)
}

// UpdateUserTrafficBatch adds cycle and lifetime user usage in one statement
// per chunk. Daily ledger rows are written separately by AddUserDailyTrafficBatch.
func UpdateUserTrafficBatch(tx DBTX, rows []UserTrafficUpdate) error {
	vals := make([][]any, len(rows))
	for i, r := range rows {
		vals[i] = []any{r.UserID, r.Cycle, r.Total}
	}
	return execUpdateFrom(tx, `UPDATE users SET
		traffic_used_bytes = users.traffic_used_bytes + v.cycle_delta,
		total_traffic_used_bytes = users.total_traffic_used_bytes + v.total_delta`,
		[]string{"id", "cycle_delta", "total_delta"},
		"users.id = v.id", vals)
}

// UpdateRuleExitBytesBatch adds final-hop raw bytes onto rules.exit_bytes.
func UpdateRuleExitBytesBatch(tx DBTX, rows []IDDelta) error {
	vals := make([][]any, len(rows))
	for i, r := range rows {
		vals[i] = []any{r.ID, r.Delta}
	}
	return execUpdateFrom(tx, `UPDATE rules SET exit_bytes = rules.exit_bytes + v.delta`,
		[]string{"id", "delta"}, "rules.id = v.id", vals)
}

// UpdateLandingExitUsedBatch adds raw bytes to landing-exit ledgers and stamps
// updatedAt on every row in the batch. A missing row (deleted between load and
// flush) changes nothing.
func UpdateLandingExitUsedBatch(tx DBTX, rows []LandingExitTrafficUpdate, updatedAt int64) error {
	if len(rows) == 0 {
		return nil
	}
	for _, part := range chunkRows(len(rows), counterBatchChunk) {
		slice := rows[part[0]:part[1]]
		var b strings.Builder
		b.WriteString(`UPDATE user_landing_exits SET used_bytes = user_landing_exits.used_bytes + v.delta, updated_at = ? FROM (`)
		args := make([]any, 0, 1+len(slice)*4)
		args = append(args, updatedAt)
		for i, r := range slice {
			if i > 0 {
				b.WriteString(" UNION ALL ")
			}
			b.WriteString("SELECT ? AS user_id, ? AS host, ? AS port, ? AS delta")
			args = append(args, r.UserID, r.Host, r.Port, r.Delta)
		}
		b.WriteString(`) AS v WHERE user_landing_exits.user_id = v.user_id AND user_landing_exits.host = v.host AND user_landing_exits.port = v.port`)
		if _, err := tx.Exec(b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

// AddUserDailyTrafficBatch folds each user's cycle delta into today's
// Asia/Shanghai ledger. Zero deltas are skipped by the caller.
func AddUserDailyTrafficBatch(tx DBTX, rows []UserTrafficUpdate) error {
	kept := rows[:0:0]
	for _, r := range rows {
		if r.Cycle != 0 {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	day := dayKey(time.Now())
	for _, part := range chunkRows(len(kept), counterBatchChunk) {
		slice := kept[part[0]:part[1]]
		var b strings.Builder
		b.WriteString(`INSERT INTO daily_user_traffic(day, user_id, raw_bytes) VALUES `)
		args := make([]any, 0, len(slice)*3)
		for i, r := range slice {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("(?,?,?)")
			args = append(args, day, r.UserID, r.Cycle)
		}
		b.WriteString(` ON CONFLICT(day, user_id) DO UPDATE SET raw_bytes = raw_bytes + excluded.raw_bytes`)
		if _, err := tx.Exec(b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func execUpdateFrom(tx DBTX, head string, cols []string, where string, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	for _, part := range chunkRows(len(rows), counterBatchChunk) {
		slice := rows[part[0]:part[1]]
		var b strings.Builder
		b.WriteString(head)
		b.WriteString(" FROM (")
		args := make([]any, 0, len(slice)*len(cols))
		for i, row := range slice {
			if len(row) != len(cols) {
				return fmt.Errorf("counter batch: row width %d, want %d", len(row), len(cols))
			}
			if i > 0 {
				b.WriteString(" UNION ALL ")
			}
			b.WriteString("SELECT ")
			for c, name := range cols {
				if c > 0 {
					b.WriteString(", ")
				}
				b.WriteString("? AS ")
				b.WriteString(name)
			}
			args = append(args, row...)
		}
		b.WriteString(") AS v WHERE ")
		b.WriteString(where)
		if _, err := tx.Exec(b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func chunkRows(n, size int) [][2]int {
	if n == 0 {
		return nil
	}
	if size <= 0 {
		size = n
	}
	out := make([][2]int, 0, (n+size-1)/size)
	for i := 0; i < n; i += size {
		j := i + size
		if j > n {
			j = n
		}
		out = append(out, [2]int{i, j})
	}
	return out
}
