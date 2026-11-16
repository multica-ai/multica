CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_project_status
    ON issue (project_id, workspace_id, status);
