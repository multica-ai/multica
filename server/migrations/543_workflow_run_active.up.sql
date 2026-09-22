CREATE INDEX CONCURRENTLY workflow_run_active_idx ON workflow_run (status, updated_at);
