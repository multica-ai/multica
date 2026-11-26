-- name: GetCommentIdempotencyReceipt :one
SELECT issue_id, comment_id, request_fingerprint FROM comment_idempotency_receipt
WHERE workspace_id = $1 AND actor_type = $2 AND actor_id = $3 AND idempotency_key = $4;

-- name: ClaimCommentIdempotencyReceipt :execrows
-- Runs first in the comment's own transaction. A concurrent request with the
-- same key waits on the unique index until the first one commits or rolls
-- back; it then claims nothing (0 rows) and replays the first comment, or
-- claims the key itself if the first request rolled back.
INSERT INTO comment_idempotency_receipt (
    workspace_id, actor_type, actor_id, idempotency_key, issue_id, comment_id, request_fingerprint
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id, actor_type, actor_id, idempotency_key) DO NOTHING;

-- name: DeleteCommentIdempotencyReceiptsBefore :execrows
-- Bounded retention sweep. Receipts only need to outlive a client's retry
-- window, so old rows are deleted in batches instead of kept forever.
DELETE FROM comment_idempotency_receipt
WHERE ctid = ANY(ARRAY(
    SELECT r.ctid FROM comment_idempotency_receipt r
    WHERE r.created_at < sqlc.arg(before)::timestamptz
    ORDER BY r.created_at
    LIMIT sqlc.arg(batch_size)::int
));
