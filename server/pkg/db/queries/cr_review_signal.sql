-- name: InsertCRReviewSignal :exec
INSERT INTO cr_review_signal (attempt_id, signal_kind, signal_action, payload_summary)
VALUES ($1, $2, $3, $4);

-- name: ListCRSignalsForAttempt :many
SELECT * FROM cr_review_signal
WHERE attempt_id = $1
ORDER BY received_at;
