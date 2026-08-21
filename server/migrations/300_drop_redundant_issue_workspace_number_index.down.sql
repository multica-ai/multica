CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_number
    ON issue (workspace_id, number);
