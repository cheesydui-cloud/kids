-- Remembers which offline / sync-error alerts already went out, so a panel
-- restart does not send the same notice again.
CREATE TABLE node_alert_state (
  node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  offline_since INTEGER NOT NULL DEFAULT 0,
  offline_alerted INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT ''
);
