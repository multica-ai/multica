-- name: UpsertDailyReview :one
INSERT INTO daily_review (workspace_id, user_id, review_date, draft_content, generated_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, user_id, review_date)
DO UPDATE SET
    draft_content = EXCLUDED.draft_content,
    generated_by = EXCLUDED.generated_by,
    status = 'draft',
    confirmed_at = NULL,
    updated_at = now()
RETURNING *;

-- name: GetDailyReviewByDate :one
SELECT * FROM daily_review
WHERE workspace_id = $1 AND user_id = $2 AND review_date = $3;

-- name: GetDailyReviewByID :one
SELECT * FROM daily_review
WHERE id = $1 AND workspace_id = $2;

-- name: ListDailyReviews :many
SELECT * FROM daily_review
WHERE workspace_id = $1 AND user_id = $2
ORDER BY review_date DESC
LIMIT $3;

-- name: ConfirmDailyReview :one
UPDATE daily_review
SET
    status = 'confirmed',
    confirmed_at = now(),
    energy_level = sqlc.narg('energy_level'),
    energy_note = sqlc.narg('energy_note'),
    recovery_need = sqlc.narg('recovery_need'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;
