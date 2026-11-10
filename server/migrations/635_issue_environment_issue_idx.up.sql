-- The issue sidebar's environment list.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_environment_issue
    ON issue_environment (issue_id, created_at);
