-- Manual display order for users and landing-repo entries, controlled by
-- drag-and-drop in the admin lists. Users keep the current id order; repo
-- entries keep the current newest-first (created_at DESC) order until an
-- admin reorders. Lists sort by (sort_order, id).
ALTER TABLE users ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
UPDATE users SET sort_order = id;

ALTER TABLE node_repo ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
UPDATE node_repo SET sort_order = (
	SELECT COUNT(*) FROM node_repo n2
	WHERE n2.created_at > node_repo.created_at
	   OR (n2.created_at = node_repo.created_at AND n2.id > node_repo.id)
) + 1;
