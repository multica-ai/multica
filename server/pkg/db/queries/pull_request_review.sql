-- name: UpsertPullRequestReview :one
-- Idempotent on the (pr, reviewer, submitted_at) triple — GitHub
-- redeliveries land on the same row. Body / state / avatar are refreshed
-- because GitHub's "edited" event reuses submitted_at but mutates the
-- text and state.
INSERT INTO pull_request_review (
    workspace_id, pull_request_id, reviewer_login, reviewer_avatar_url,
    state, body, submitted_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (pull_request_id, reviewer_login, submitted_at) DO UPDATE SET
    reviewer_avatar_url = EXCLUDED.reviewer_avatar_url,
    state               = EXCLUDED.state,
    body                = EXCLUDED.body
RETURNING *;

-- name: ListReviewsForPR :many
-- Ordered newest-first so the derivation step can fold over the latest
-- review per reviewer in a single pass.
SELECT * FROM pull_request_review
WHERE pull_request_id = $1
ORDER BY submitted_at DESC;
