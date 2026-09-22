CREATE INDEX CONCURRENTLY workflow_run_workspace_idx ON workflow_run (workspace_id, workflow_id, created_at DESC);
