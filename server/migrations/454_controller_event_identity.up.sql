CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS controller_event_identity ON controller_event (workspace_id, issue_id, event_id);
