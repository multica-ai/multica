-- Workspace-wide audit timeline of approval events (newest first). Keep this
-- as the migration's only statement: PostgreSQL rejects CREATE INDEX
-- CONCURRENTLY inside a transaction or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_approval_event_workspace
    ON approval_event (workspace_id, created_at DESC);
