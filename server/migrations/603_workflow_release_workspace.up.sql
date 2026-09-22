CREATE INDEX CONCURRENTLY workflow_release_workspace_idx ON workflow_release (workspace_id, workflow_id, created_at DESC);
