CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS daemon_enrollment_workspace_daemon_idx ON daemon_enrollment (workspace_id, daemon_id);
