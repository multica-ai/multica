DROP INDEX IF EXISTS idx_comment_idempotency_pending_claim;

CREATE INDEX idx_comment_idempotency_pending_claim
    ON comment_idempotency (side_effects_claimed_at, created_at)
    WHERE side_effects_completed_at IS NULL;
