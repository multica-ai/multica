-- name: RecordWebhookDelivery :one
-- At-most-once dedup: an identical X-GitHub-Delivery from a retry lands
-- on the existing row. The :one return shape lets the handler tell
-- "first time" (newly inserted) from "duplicate" (xmax != 0 trick) by
-- comparing received_at to processed_at — the dedup gate uses ON
-- CONFLICT DO NOTHING semantics and a row-count check via the SELECT
-- that follows.
--
-- Implementation note: we use ON CONFLICT DO UPDATE on a no-op so the
-- INSERT always returns the row, then the caller decides how to act.
-- "DO NOTHING" with RETURNING would skip the row on conflict.
INSERT INTO github_webhook_delivery (delivery_id, event_type, workspace_id, repo_url)
VALUES ($1, $2, $3, $4)
ON CONFLICT (delivery_id) DO UPDATE SET delivery_id = github_webhook_delivery.delivery_id
RETURNING *,
    -- xmax = 0 means "this row was inserted by THIS statement"; >0 means
    -- the conflicting row already existed. The handler treats nonzero as
    -- a duplicate retry.
    (xmax = 0) AS inserted;

-- name: MarkWebhookDeliveryProcessed :exec
-- Stamp processed_at after async processing completes. error is nullable;
-- a non-empty value records the failure but doesn't block dedup — we
-- accept that a webhook with a transient failure is dropped on retry,
-- which matches GitHub's own at-least-once delivery model.
UPDATE github_webhook_delivery SET
    processed_at = now(),
    error        = sqlc.narg('error')
WHERE delivery_id = $1;
