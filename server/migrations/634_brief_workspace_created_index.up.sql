CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_brief_workspace_created ON brief (workspace_id, created_at DESC);
