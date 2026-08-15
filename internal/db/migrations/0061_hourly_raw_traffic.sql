-- Rolling 24h actual-traffic buckets for the ops overview curve.
-- Independent of billing multipliers and user traffic resets.
CREATE TABLE IF NOT EXISTS hourly_raw_traffic (
    hour      TEXT    NOT NULL PRIMARY KEY,
    raw_bytes INTEGER NOT NULL DEFAULT 0
);
