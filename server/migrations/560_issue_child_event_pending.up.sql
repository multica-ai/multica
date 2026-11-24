CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_child_event_pending_idx ON issue_child_event(parent_id,created_at) WHERE processed_at IS NULL;
