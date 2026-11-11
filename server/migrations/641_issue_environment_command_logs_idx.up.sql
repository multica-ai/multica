-- The sweep that deletes log reads once their output is no longer needed.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_environment_command_logs
    ON issue_environment_command (created_at)
    WHERE action = 'logs';
