-- Shared cap for one user on one node, in megabits/s. 0 means unlimited.
-- This is a new column. The retired rate_limit_mbytes values stay in place
-- and are not copied here, so an upgrade does not start shaping live traffic.
ALTER TABLE user_nodes ADD COLUMN rate_mbps INTEGER NOT NULL DEFAULT 0;
