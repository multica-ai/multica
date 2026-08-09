CREATE UNIQUE INDEX CONCURRENTLY corpus_transfer_idempotency_uidx ON corpus_transfer (workspace_id, actor_id, idempotency_key);
