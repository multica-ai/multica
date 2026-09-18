-- Match the recovery scan's pending predicate and oldest-first ordering. The
-- claim-lease index remains useful for lease diagnostics; this index keeps the
-- normal bounded recovery query index-backed as the replay table grows.
DROP INDEX IF EXISTS idx_comment_idempotency_pending_claim;

CREATE INDEX idx_comment_idempotency_pending_claim
    ON comment_idempotency (created_at, workspace_id, idempotency_key)
    WHERE side_effects_completed_at IS NULL;
