-- name: CreateNotificationEvent :one
INSERT INTO notification_event (
    workspace_id,
    recipient_user_id,
    type,
    severity,
    issue_id,
    comment_id,
    actor_type,
    actor_id,
    title,
    body,
    link,
    details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetNotificationEvent :one
SELECT * FROM notification_event
WHERE id = $1;

-- name: ListNotificationEventsByRecipient :many
SELECT * FROM notification_event
WHERE workspace_id = $1 AND recipient_user_id = $2
ORDER BY created_at DESC;

-- name: CreateNotificationDelivery :one
INSERT INTO notification_delivery (
    notification_event_id,
    channel,
    status,
    attempt_count,
    last_error,
    payload_snapshot,
    sent_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateTargetedNotificationDelivery :one
INSERT INTO notification_delivery (
    notification_event_id,
    channel,
    target_type,
    target_id,
    status,
    attempt_count,
    last_error,
    payload_snapshot,
    sent_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListNotificationDeliveriesByEvent :many
SELECT * FROM notification_delivery
WHERE notification_event_id = $1
ORDER BY created_at ASC;

-- name: ListNotificationDeliveriesByStatus :many
SELECT * FROM notification_delivery
WHERE status = $1
ORDER BY created_at ASC;

-- name: ListNotificationDebugRows :many
SELECT
    ne.id AS notification_event_id,
    ne.workspace_id,
    ne.recipient_user_id,
    ne.type,
    ne.severity,
    ne.issue_id,
    ne.comment_id,
    ne.actor_type,
    ne.actor_id,
    ne.title,
    ne.body,
    ne.link,
    ne.details,
    ne.created_at AS event_created_at,
    nd.id AS delivery_id,
    nd.channel,
    nd.status,
    nd.attempt_count,
    nd.last_error,
    nd.payload_snapshot,
    nd.sent_at,
    nd.created_at AS delivery_created_at,
    nd.updated_at AS delivery_updated_at,
    nd.target_type,
    nd.target_id
FROM notification_event ne
LEFT JOIN notification_delivery nd
  ON nd.notification_event_id = ne.id
 AND (sqlc.narg('channel')::text IS NULL OR nd.channel = sqlc.narg('channel')::text)
WHERE ne.workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('issue_id')::uuid IS NULL OR ne.issue_id = sqlc.narg('issue_id')::uuid)
  AND (sqlc.narg('recipient_user_id')::uuid IS NULL OR ne.recipient_user_id = sqlc.narg('recipient_user_id')::uuid)
  AND (sqlc.narg('comment_id')::uuid IS NULL OR ne.comment_id = sqlc.narg('comment_id')::uuid)
  AND (sqlc.narg('event_type')::text IS NULL OR ne.type = sqlc.narg('event_type')::text)
ORDER BY ne.created_at DESC, nd.created_at DESC NULLS LAST
LIMIT LEAST(GREATEST(sqlc.arg('limit')::int, 1), 200);

-- name: ClaimNotificationDelivery :one
UPDATE notification_delivery
SET status = $2,
    attempt_count = attempt_count + 1,
    last_error = NULL,
    updated_at = now()
WHERE id = $1 AND status = $3
RETURNING *;

-- name: CompleteNotificationDelivery :one
UPDATE notification_delivery
SET status = $2,
    last_error = $3,
    sent_at = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListExternalAccountBindingsByUser :many
SELECT * FROM external_account_binding
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: GetExternalAccountBinding :one
SELECT * FROM external_account_binding
WHERE id = $1;

-- name: GetExternalAccountBindingByProviderAndExternalID :one
SELECT * FROM external_account_binding
WHERE provider = $1 AND external_user_id = $2;

-- name: GetExternalAccountBindingByUserAndProvider :one
SELECT * FROM external_account_binding
WHERE user_id = $1 AND provider = $2;

-- name: UpsertExternalAccountBinding :one
INSERT INTO external_account_binding (
    user_id,
    provider,
    external_user_id,
    display_name,
    access_token_encrypted,
    refresh_token_encrypted,
    token_expires_at,
    status,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (user_id, provider)
DO UPDATE SET
    external_user_id = EXCLUDED.external_user_id,
    display_name = EXCLUDED.display_name,
    access_token_encrypted = EXCLUDED.access_token_encrypted,
    refresh_token_encrypted = EXCLUDED.refresh_token_encrypted,
    token_expires_at = EXCLUDED.token_expires_at,
    status = EXCLUDED.status,
    metadata = EXCLUDED.metadata,
    updated_at = now()
RETURNING *;

-- name: ListNotificationChannelPreferencesByUser :many
SELECT * FROM notification_channel_preference
WHERE user_id = $1
ORDER BY channel ASC, event_type ASC;

-- name: UpsertNotificationChannelPreference :one
INSERT INTO notification_channel_preference (
    user_id,
    channel,
    event_type,
    enabled,
    binding_id,
    render_mode
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (user_id, channel, event_type)
DO UPDATE SET
    enabled = EXCLUDED.enabled,
    binding_id = EXCLUDED.binding_id,
    render_mode = EXCLUDED.render_mode,
    updated_at = now()
RETURNING *;

-- name: ExistsOpenclawWeixinDeliveryByComment :one
SELECT EXISTS (
    SELECT 1
    FROM notification_delivery nd
    JOIN notification_event ne ON ne.id = nd.notification_event_id
    WHERE ne.recipient_user_id = $1
      AND ne.comment_id = $2
      AND nd.channel = 'openclaw_weixin'
      AND nd.status IN ('pending', 'sent')
) AS "exists";

-- name: ExistsDeliveryByCommentAndChannel :one
SELECT EXISTS (
    SELECT 1
    FROM notification_delivery nd
    JOIN notification_event ne ON ne.id = nd.notification_event_id
    WHERE ne.recipient_user_id = $1
      AND ne.comment_id = $2
      AND nd.channel = $3
      AND nd.status IN ('pending', 'sent')
) AS "exists";
