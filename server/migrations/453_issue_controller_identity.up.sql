CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_controller_identity ON issue_controller (workspace_id, issue_id);
