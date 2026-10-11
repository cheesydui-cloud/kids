-- Stable machine identity for the landing warehouse. Protocols on one
-- machine share machine_id. Current IP stays a swappable address.
-- Probe columns keep the last DNS check on the row so a refresh still shows it.
ALTER TABLE node_repo ADD COLUMN machine_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_repo ADD COLUMN dns_probe_status TEXT NOT NULL DEFAULT '';
ALTER TABLE node_repo ADD COLUMN dns_probe_message TEXT NOT NULL DEFAULT '';
ALTER TABLE node_repo ADD COLUMN dns_probe_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_repo ADD COLUMN dns_probe_ip TEXT NOT NULL DEFAULT '';
