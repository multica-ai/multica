-- The issue sidebar's pending and most recent actions per environment.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_environment_command_issue
    ON issue_environment_command (issue_id, created_at);
