-- Workspace-scoped listing of approval requests (newest first). Keep this as
-- the migration's only statement: PostgreSQL rejects CREATE INDEX CONCURRENTLY
-- inside a transaction or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_approval_request_workspace
    ON approval_request (workspace_id, created_at DESC);
