CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pat_workspace ON personal_access_token (workspace_id) WHERE workspace_id IS NOT NULL;
