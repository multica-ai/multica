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

-- name: ListNotificationDeliveriesByEvent :many
SELECT * FROM notification_delivery
WHERE notification_event_id = $1
ORDER BY created_at ASC;

-- name: ListNotificationDeliveriesByStatus :many
SELECT * FROM notification_delivery
WHERE status = $1
ORDER BY created_at ASC;

-- name: ListExternalAccountBindingsByUser :many
SELECT * FROM external_account_binding
WHERE user_id = $1
ORDER BY created_at ASC;

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
    binding_id
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, channel, event_type)
DO UPDATE SET
    enabled = EXCLUDED.enabled,
    binding_id = EXCLUDED.binding_id,
    updated_at = now()
RETURNING *;
