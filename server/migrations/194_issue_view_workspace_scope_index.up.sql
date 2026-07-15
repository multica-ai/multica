-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction. The migration runner executes single statements directly.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_view_workspace_scope
    ON issue_view (workspace_id, scope_type, scope_id, position, created_at);
