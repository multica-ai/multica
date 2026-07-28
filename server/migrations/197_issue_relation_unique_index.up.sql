-- Enforce one row per directed edge and serve both the source-issue lookup and
-- the 'blocks' reachability traversal (which filter workspace_id + source_issue_id
-- + type). Column order (workspace_id, source_issue_id, type, target_issue_id)
-- is chosen for those access patterns; leading with workspace_id keeps the index
-- covering for the workspace-scoped queries (matches the issue_property index
-- convention). source_issue_id determines the workspace, so this key is
-- equivalent to (source, target, type) for uniqueness. Keep this as the
-- migration's only statement: PostgreSQL rejects CREATE INDEX CONCURRENTLY
-- inside a transaction or multi-command string.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_relation_unique
    ON issue_relation (workspace_id, source_issue_id, type, target_issue_id);
