-- The log is read newest first within a workspace, with filters and a cursor.
CREATE INDEX CONCURRENTLY IF NOT EXISTS governance_event_workspace_created_idx ON governance_event (workspace_id, created_at DESC);
