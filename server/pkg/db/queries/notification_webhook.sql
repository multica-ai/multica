-- name: CreateNotificationWebhookEndpoint :one
INSERT INTO notification_webhook_endpoint (
    user_id,
    workspace_id,
    name,
    url_encrypted,
    secret_encrypted,
    payload_template,
    content_prefix,
    enabled
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListNotificationWebhookEndpointsByUser :many
SELECT * FROM notification_webhook_endpoint
WHERE user_id = $1
ORDER BY created_at ASC;

-- name: ListEnabledNotificationWebhookEndpointsByUser :many
SELECT * FROM notification_webhook_endpoint
WHERE user_id = $1 AND enabled = true
ORDER BY created_at ASC;

-- name: GetNotificationWebhookEndpoint :one
SELECT * FROM notification_webhook_endpoint
WHERE id = $1;

-- name: GetNotificationWebhookEndpointForUser :one
SELECT * FROM notification_webhook_endpoint
WHERE id = $1 AND user_id = $2;

-- name: UpdateNotificationWebhookEndpoint :one
UPDATE notification_webhook_endpoint
SET name = $3,
    url_encrypted = $4,
    secret_encrypted = $5,
    payload_template = $6,
    content_prefix = $7,
    enabled = $8,
    updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteNotificationWebhookEndpoint :exec
DELETE FROM notification_webhook_endpoint
WHERE id = $1 AND user_id = $2;
