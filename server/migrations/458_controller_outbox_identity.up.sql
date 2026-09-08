CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS controller_outbox_identity ON controller_outbox (workspace_id, event_id);
