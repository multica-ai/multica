-- =====================
-- Webhook Subscription CRUD (RFC #1964)
-- =====================

-- name: ListWebhookSubscriptions :many
SELECT * FROM webhook_subscription
WHERE workspace_id = $1
  AND (sqlc.narg('state')::text IS NULL OR state = sqlc.narg('state'))
ORDER BY created_at DESC;

-- name: GetWebhookSubscription :one
SELECT * FROM webhook_subscription
WHERE id = $1;

-- name: GetWebhookSubscriptionInWorkspace :one
SELECT * FROM webhook_subscription
WHERE id = $1 AND workspace_id = $2;

-- name: CreateWebhookSubscription :one
INSERT INTO webhook_subscription (
    workspace_id, name, url, secret, event_filter,
    state, pause_threshold, allow_http,
    per_attempt_timeout_seconds, event_taxonomy_pinned_at,
    created_by
) VALUES (
    $1, $2, $3, $4, $5,
    COALESCE(sqlc.narg('state')::text, 'active'),
    COALESCE(sqlc.narg('pause_threshold')::int, 5),
    COALESCE(sqlc.narg('allow_http')::boolean, FALSE),
    COALESCE(sqlc.narg('per_attempt_timeout_seconds')::int, 10),
    sqlc.narg('event_taxonomy_pinned_at'),
    $6
)
RETURNING *;

-- name: UpdateWebhookSubscription :one
UPDATE webhook_subscription SET
    name = COALESCE(sqlc.narg('name'), name),
    url = COALESCE(sqlc.narg('url'), url),
    event_filter = COALESCE(sqlc.narg('event_filter')::text[], event_filter),
    state = COALESCE(sqlc.narg('state'), state),
    pause_threshold = COALESCE(sqlc.narg('pause_threshold')::int, pause_threshold),
    allow_http = COALESCE(sqlc.narg('allow_http')::boolean, allow_http),
    per_attempt_timeout_seconds = COALESCE(sqlc.narg('per_attempt_timeout_seconds')::int, per_attempt_timeout_seconds),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RotateWebhookSubscriptionSecret :one
UPDATE webhook_subscription SET
    secret = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: BumpWebhookConsecutiveFailures :one
UPDATE webhook_subscription SET
    consecutive_failures = consecutive_failures + 1,
    state = CASE
        WHEN consecutive_failures + 1 >= pause_threshold AND state = 'active' THEN 'auto_paused'
        ELSE state
    END,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ResetWebhookConsecutiveFailures :exec
UPDATE webhook_subscription SET
    consecutive_failures = 0,
    updated_at = now()
WHERE id = $1;

-- name: DeleteWebhookSubscription :exec
DELETE FROM webhook_subscription WHERE id = $1;

-- =====================
-- Webhook Delivery CRUD (RFC #1964)
-- =====================

-- name: CreateWebhookDelivery :one
INSERT INTO webhook_delivery (
    subscription_id, event_id, event_type, payload,
    status, next_attempt_at
) VALUES (
    $1, $2, $3, $4,
    COALESCE(sqlc.narg('status')::text, 'pending'),
    COALESCE(sqlc.narg('next_attempt_at')::timestamptz, now())
)
RETURNING *;

-- ClaimPendingWebhookDeliveries pulls a batch of deliveries the dispatcher
-- should run. SKIP LOCKED makes it safe to call from concurrent workers
-- (relevant if/when the dispatcher is later scaled across instances).
-- name: ClaimPendingWebhookDeliveries :many
SELECT d.* FROM webhook_delivery d
JOIN webhook_subscription s ON s.id = d.subscription_id
WHERE d.status = 'pending'
  AND (d.next_attempt_at IS NULL OR d.next_attempt_at <= now())
  AND s.state = 'active'
ORDER BY d.next_attempt_at ASC NULLS FIRST
LIMIT $1
FOR UPDATE OF d SKIP LOCKED;

-- name: ListWebhookDeliveries :many
SELECT * FROM webhook_delivery
WHERE subscription_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: GetWebhookDelivery :one
SELECT * FROM webhook_delivery
WHERE id = $1;

-- name: MarkWebhookDeliverySucceeded :exec
UPDATE webhook_delivery SET
    status = 'succeeded',
    attempt = attempt + 1,
    last_response_status = sqlc.arg('last_response_status')::int,
    last_response_body_truncated = sqlc.narg('last_response_body_truncated'),
    last_error = NULL,
    completed_at = now()
WHERE id = $1;

-- name: ScheduleWebhookDeliveryRetry :exec
UPDATE webhook_delivery SET
    status = 'pending',
    attempt = attempt + 1,
    next_attempt_at = sqlc.arg('next_attempt_at')::timestamptz,
    last_response_status = sqlc.narg('last_response_status'),
    last_response_body_truncated = sqlc.narg('last_response_body_truncated'),
    last_error = sqlc.narg('last_error')
WHERE id = $1;

-- name: MarkWebhookDeliveryDead :exec
UPDATE webhook_delivery SET
    status = 'dead',
    attempt = attempt + 1,
    last_response_status = sqlc.narg('last_response_status'),
    last_response_body_truncated = sqlc.narg('last_response_body_truncated'),
    last_error = sqlc.narg('last_error'),
    completed_at = now()
WHERE id = $1;

-- name: CountPendingWebhookDeliveriesForSubscription :one
SELECT COUNT(*) FROM webhook_delivery
WHERE subscription_id = $1 AND status = 'pending';

-- DropOldestPendingWebhookDeliveries enforces a per-subscription backpressure
-- cap. When `onEvent` is about to insert a fresh delivery and the pending
-- count is already at or past the cap, this deletes the N oldest pending
-- rows to make room. Per RFC #1964 Q2: drop-oldest is acceptable behind a
-- persisted deliveries table — durable so the audit trail survives.
-- name: DropOldestPendingWebhookDeliveries :exec
DELETE FROM webhook_delivery
WHERE id IN (
    SELECT inner_d.id FROM webhook_delivery inner_d
    WHERE inner_d.subscription_id = sqlc.arg('subscription_id')
      AND inner_d.status = 'pending'
    ORDER BY inner_d.created_at ASC
    LIMIT sqlc.arg('drop_count')
);
