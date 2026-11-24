CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_idempotency_receipt_created ON comment_idempotency_receipt (created_at);
