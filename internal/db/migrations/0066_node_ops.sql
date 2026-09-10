-- Admin-only operations metadata for line-monitoring nodes: folder grouping,
-- free-text remark, renewal date and monthly cost. Additive only; existing
-- rows keep their behaviour with defaults.
ALTER TABLE nodes ADD COLUMN group_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN group_name TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN remark TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN expires_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN monthly_cost_cents INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS node_folders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
