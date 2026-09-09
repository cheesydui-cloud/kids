-- Operational indexes for bounded history cleanup and audit browsing.
-- These are additive only; no business rows are changed.
CREATE INDEX IF NOT EXISTS idx_audit_logs_at ON audit_logs(at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_at ON audit_logs(user_id, at);
CREATE INDEX IF NOT EXISTS idx_rule_hops_rule_position ON rule_hops(rule_id, position);
CREATE INDEX IF NOT EXISTS idx_daily_user_traffic_day ON daily_user_traffic(day);
CREATE INDEX IF NOT EXISTS idx_daily_node_raw_traffic_day ON daily_node_raw_traffic(day);
