-- Per-user announcement read receipts, so the unread badge follows the account
-- across browsers/devices instead of living only in localStorage.
CREATE TABLE IF NOT EXISTS announcement_reads (
  user_id INTEGER NOT NULL,
  announcement_id INTEGER NOT NULL,
  read_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, announcement_id)
);
CREATE INDEX IF NOT EXISTS idx_announcement_reads_user ON announcement_reads(user_id);
