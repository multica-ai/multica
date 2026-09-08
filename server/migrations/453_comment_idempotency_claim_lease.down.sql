DROP INDEX IF EXISTS idx_comment_idempotency_pending_claim;

ALTER TABLE comment_idempotency
    DROP COLUMN IF EXISTS side_effects_claimed_at;
