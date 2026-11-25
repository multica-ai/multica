-- Receipts that make POST /api/issues/{id}/comments idempotent (GH #8221).
--
-- A client that sends an Idempotency-Key gets one row here, written in the
-- same transaction as the comment. A retry with the same key from the same
-- author returns the saved comment instead of posting it again, so a client
-- that lost the response can resend safely.
--
-- The comment table itself is not touched: no new column or index on a hot
-- table. No foreign keys: rows are removed by the retention sweeper and by
-- workspace deletion in application code. A receipt whose comment was later
-- deleted is still honoured; the retry is refused instead of re-posting.
CREATE TABLE IF NOT EXISTS comment_idempotency_receipt (
    workspace_id UUID NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id UUID NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    issue_id UUID NOT NULL,
    comment_id UUID NOT NULL,
    request_fingerprint BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
