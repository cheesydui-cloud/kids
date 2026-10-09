-- A user asks the admin to renew the account or add quota. One open row per
-- kind is enough; the admin marks it done from the user page.
CREATE TABLE IF NOT EXISTS user_requests (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'open',
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_user_requests_user ON user_requests(user_id, status, created_at);
