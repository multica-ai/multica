-- =====================
-- Webhook Delivery
-- =====================

-- name: CreateWebhookDelivery :one
-- Inserts a delivery row. On dedupe-key collision the unique partial index
-- (trigger_id, dedupe_key) raises 23505 and the handler treats it as
-- "duplicate" rather than an error.
INSERT INTO webhook_delivery (
    workspace_id, autopilot_id, trigger_id, provider, event,
    dedupe_key, dedupe_source, signature_status, status,
    selected_headers, content_type, raw_body,
    replayed_from_delivery_id
) VALUES (
    $1, $2, $3, $4, $5,
    sqlc.narg('dedupe_key'), sqlc.narg('dedupe_source'), $6, $7,
    $8, sqlc.narg('content_type'), sqlc.narg('raw_body'),
    sqlc.narg('replayed_from_delivery_id')
) RETURNING *;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_delivery
WHERE id = $1;

-- name: GetWebhookDeliveryInWorkspace :one
-- Workspace-scoped read for authenticated detail / replay endpoints.
SELECT * FROM webhook_delivery
WHERE id = $1 AND workspace_id = $2;

-- name: GetWebhookDeliveryByTriggerAndDedupe :one
-- Looks up the existing delivery for a (trigger, dedupe_key) pair so that
-- duplicate requests return the original delivery_id / autopilot_run_id.
-- The partial unique index excludes terminal-but-not-successful statuses
-- (`rejected`, `failed`), so multiple such rows can coexist for the same
-- key. Prefer non-terminal rows in the lookup: without the ORDER BY we
-- could return a stale rejection / failure even after the operator fixed
-- the cause and a fresh dispatch succeeded.
SELECT * FROM webhook_delivery
WHERE trigger_id = $1
  AND dedupe_key = $2
ORDER BY (status IN ('rejected', 'failed')), created_at DESC
LIMIT 1;

-- name: BumpWebhookDeliveryAttempt :one
-- On duplicate detection, bump attempt_count and refresh last_attempt_at on
-- the existing delivery so the UI / operator can see retry pressure without
-- creating a new row per attempt.
UPDATE webhook_delivery
SET attempt_count = attempt_count + 1,
    last_attempt_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWebhookDeliveryDispatched :one
-- Finalises a delivery that successfully created (or skipped to) an
-- autopilot_run. response_status is the HTTP status we returned, recorded
-- alongside so the operator can correlate logs.
UPDATE webhook_delivery
SET status = $2,
    autopilot_run_id = sqlc.narg('autopilot_run_id'),
    response_status = sqlc.narg('response_status'),
    response_body = sqlc.narg('response_body'),
    last_attempt_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWebhookDeliveryTerminal :one
-- Finalises a delivery without an autopilot_run link — rejected, ignored,
-- failed. Separate query so callers can't accidentally drop the run_id when
-- they only meant to set status/error.
UPDATE webhook_delivery
SET status = $2,
    error = sqlc.narg('error'),
    response_status = sqlc.narg('response_status'),
    response_body = sqlc.narg('response_body'),
    last_attempt_at = now()
WHERE id = $1
RETURNING *;

-- name: ListWebhookDeliveriesByAutopilot :many
-- Workspace-scoped via the join so a runId from another workspace cannot
-- leak. Newest first, paged by limit/offset.
--
-- Projection: large columns (`raw_body`, `selected_headers`, `response_body`)
-- are deliberately excluded. A 100-row page × 256 KiB raw_body would be
-- 25 MiB of bytes pulled from Postgres just to be dropped in the JSON
-- encoder — Deliveries tab would hit that on every reload. Detail views
-- fetch the full row via GetWebhookDelivery / GetWebhookDeliveryInWorkspace.
SELECT
    d.id, d.workspace_id, d.autopilot_id, d.trigger_id, d.provider, d.event,
    d.dedupe_key, d.dedupe_source, d.signature_status, d.status,
    d.attempt_count, d.content_type, d.response_status,
    d.autopilot_run_id, d.replayed_from_delivery_id, d.error,
    d.received_at, d.last_attempt_at, d.created_at
FROM webhook_delivery d
JOIN autopilot a ON a.id = d.autopilot_id
WHERE d.autopilot_id = $1
  AND a.workspace_id = $2
ORDER BY d.created_at DESC
LIMIT $3 OFFSET $4;
