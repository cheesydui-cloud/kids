-- Agent-reported operator notes. Empty on old agents; the panel only
-- writes a column when the new hello/ping actually carries the field.
ALTER TABLE nodes ADD COLUMN last_disconnect_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN last_disconnect_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE nodes ADD COLUMN egress_note TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN resource_warning TEXT NOT NULL DEFAULT '';
