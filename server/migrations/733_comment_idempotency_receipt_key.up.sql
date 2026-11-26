CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_comment_idempotency_receipt_key ON comment_idempotency_receipt (workspace_id, actor_type, actor_id, idempotency_key);
