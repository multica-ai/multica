CREATE UNIQUE INDEX CONCURRENTLY workflow_run_idempotency_idx ON workflow_run (workflow_id, creator_id, idempotency_key);
