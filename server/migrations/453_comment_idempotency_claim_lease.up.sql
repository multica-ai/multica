-- A short-lived durable claim prevents two replicas from replaying the same
-- post-commit effects concurrently. An expired claim is recoverable after a
-- process crash or an unresponsive downstream dependency.
ALTER TABLE comment_idempotency
    ADD COLUMN side_effects_claimed_at TIMESTAMPTZ;

CREATE INDEX idx_comment_idempotency_pending_claim
    ON comment_idempotency (side_effects_claimed_at, created_at)
    WHERE side_effects_completed_at IS NULL;
