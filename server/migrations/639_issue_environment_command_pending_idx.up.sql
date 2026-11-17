-- A daemon's claim of the commands queued for one of its runtimes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_environment_command_pending
    ON issue_environment_command (runtime_id, created_at)
    WHERE status = 'pending';
