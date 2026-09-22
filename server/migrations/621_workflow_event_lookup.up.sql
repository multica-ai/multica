CREATE INDEX CONCURRENTLY workflow_event_lookup_idx ON workflow_event (workspace_id, run_id, created_at, id);
