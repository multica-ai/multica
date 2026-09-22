CREATE UNIQUE INDEX CONCURRENTLY workflow_work_item_idempotency_idx ON workflow_work_item (run_id, idempotency_key) WHERE idempotency_key <> '';
