-- Support workspace-scoped property listing and counts without blocking writes.
-- Keep this as the migration's only statement: PostgreSQL rejects CREATE INDEX
-- CONCURRENTLY inside a transaction or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_property_workspace
    ON issue_property (workspace_id);
