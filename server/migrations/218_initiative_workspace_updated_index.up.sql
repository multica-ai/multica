CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_initiative_workspace_updated ON initiative (workspace_id, updated_at DESC);
