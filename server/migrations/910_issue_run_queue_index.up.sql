CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_run_queued
    ON issue (workspace_id, project_id)
    WHERE run_queued;
