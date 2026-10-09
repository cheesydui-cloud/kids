-- Lookup key for subscription tokens. The token column becomes ciphertext
-- (enc1:...) so a database backup does not contain a usable subscription URL.
-- The encryption key lives outside the database file. Empty hashes are excluded
-- from the unique index so this migration can land before the Go backfill.
ALTER TABLE sub_tokens ADD COLUMN token_hash TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_sub_tokens_token_hash ON sub_tokens(token_hash) WHERE token_hash <> '';
