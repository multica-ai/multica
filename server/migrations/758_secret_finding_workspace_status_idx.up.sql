-- The security page lists a workspace's findings by status, newest first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_secret_finding_workspace_status
    ON secret_finding (workspace_id, status, created_at DESC);
