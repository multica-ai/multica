CREATE UNIQUE INDEX CONCURRENTLY workflow_release_idempotency_idx ON workflow_release (workflow_id, created_by, idempotency_key) WHERE idempotency_key <> '';
