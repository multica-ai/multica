CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_pull_request_exclusion_workspace
    ON issue_pull_request_exclusion (workspace_id);
