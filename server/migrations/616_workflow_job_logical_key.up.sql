CREATE UNIQUE INDEX CONCURRENTLY workflow_job_logical_key_idx ON workflow_job (workspace_id, logical_key);
