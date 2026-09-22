CREATE INDEX CONCURRENTLY workflow_run_release_idx ON workflow_run (workspace_id, workflow_id, release_id, created_at DESC);
