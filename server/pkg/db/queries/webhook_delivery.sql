-- name: RecordWebhookDelivery :one
-- Returns the inserted row on first delivery, or no rows if duplicate.
-- Callers detect dedup with pgx.ErrNoRows.
INSERT INTO github_webhook_delivery (delivery_id, repo, event)
VALUES ($1, $2, $3)
ON CONFLICT (delivery_id) DO NOTHING
RETURNING *;
