-- Support ordered, bounded cleanup of expired comment replay keys.
CREATE INDEX idx_comment_idempotency_created_at
    ON comment_idempotency (created_at, workspace_id, idempotency_key);
