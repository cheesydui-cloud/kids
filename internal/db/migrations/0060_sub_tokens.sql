-- Per-user outbound subscription token. Stored in plaintext so the panel can
-- always render a stable Clash / Shadowrocket / V2rayN URL (unlike api_tokens,
-- which are hashed and only shown once at create).
CREATE TABLE IF NOT EXISTS sub_tokens (
  user_id INTEGER PRIMARY KEY,
  token TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER
);
