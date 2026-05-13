-- name: CreateNotificationDelivery :exec
INSERT INTO notification_delivery (
    inbox_item_id,
    workspace_id,
    recipient_user_id,
    channel,
    dedupe_key
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
ON CONFLICT (channel, dedupe_key) DO NOTHING;

-- name: ClaimPendingNotificationDeliveries :many
UPDATE notification_delivery
SET status = 'sending', updated_at = now()
WHERE id IN (
    SELECT id
    FROM notification_delivery
    WHERE channel = ANY(sqlc.arg('channels')::text[])
      AND (
        (status = 'pending' AND next_attempt_at <= now())
        OR (status = 'sending' AND updated_at < now() - interval '5 minutes')
      )
      AND retry_count < sqlc.arg('max_attempts')::int
    ORDER BY next_attempt_at ASC, created_at ASC
    LIMIT sqlc.arg('limit')::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: GetNotificationDeliveryMessage :one
SELECT
    d.id,
    d.inbox_item_id,
    d.workspace_id,
    d.recipient_user_id,
    d.channel,
    d.status,
    d.dedupe_key,
    d.retry_count,
    d.next_attempt_at,
    d.last_error,
    d.sent_at,
    d.created_at,
    d.updated_at,
    i.type,
    i.severity,
    i.title,
    i.body,
    i.issue_id,
    i.actor_type,
    i.actor_id,
    w.slug AS workspace_slug,
    w.issue_prefix,
    iss.number AS issue_number,
    iss.status AS issue_status,
    identity.open_id AS recipient_open_id
FROM notification_delivery d
JOIN inbox_item i ON i.id = d.inbox_item_id
JOIN workspace w ON w.id = d.workspace_id
LEFT JOIN issue iss ON iss.id = i.issue_id
LEFT JOIN LATERAL (
    SELECT x.open_id
    FROM user_external_identity x
    WHERE x.user_id = d.recipient_user_id
      AND x.provider = sqlc.arg('provider')::text
      AND x.open_id IS NOT NULL
      AND x.open_id <> ''
    ORDER BY x.last_synced_at DESC NULLS LAST, x.updated_at DESC
    LIMIT 1
) identity ON true
WHERE d.id = sqlc.arg('id')::uuid;

-- name: MarkNotificationDeliverySent :exec
UPDATE notification_delivery
SET status = 'sent',
    sent_at = now(),
    last_error = NULL,
    updated_at = now()
WHERE id = $1;

-- name: MarkNotificationDeliveryPendingAfterFailure :exec
UPDATE notification_delivery
SET status = 'pending',
    retry_count = retry_count + 1,
    next_attempt_at = now() + (sqlc.arg('delay_seconds')::int * interval '1 second'),
    last_error = left(sqlc.arg('last_error')::text, 2000),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid;

-- name: MarkNotificationDeliveryFailed :exec
UPDATE notification_delivery
SET status = 'failed',
    last_error = left(sqlc.arg('last_error')::text, 2000),
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid;
