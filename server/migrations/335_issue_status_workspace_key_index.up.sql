-- The catalog lookup that every status resolution and the ordering join use:
-- (workspace_id, key) is how `issue.status` is resolved back to its category.
-- Unique because a workspace cannot hold two statuses with the same key, and
-- this is what stops a custom status from colliding with a built-in one.
CREATE UNIQUE INDEX CONCURRENTLY idx_issue_status_workspace_key
    ON issue_status (workspace_id, key);
