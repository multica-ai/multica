-- name: UpsertMobilePushDeviceToken :one
INSERT INTO mobile_push_device_token (
    user_id, workspace_id, provider, token, device_id,
    platform, app_version, environment, enabled,
    last_registered_at, last_seen_at, updated_at
) VALUES (
    $1, $2, $3, $4, sqlc.narg('device_id'),
    $5, sqlc.narg('app_version'), $6, TRUE,
    now(), now(), now()
)
ON CONFLICT (provider, token)
DO UPDATE SET
    user_id = EXCLUDED.user_id,
    workspace_id = EXCLUDED.workspace_id,
    device_id = EXCLUDED.device_id,
    platform = EXCLUDED.platform,
    app_version = EXCLUDED.app_version,
    environment = EXCLUDED.environment,
    enabled = TRUE,
    last_registered_at = now(),
    last_seen_at = now(),
    updated_at = now()
RETURNING *;

-- name: DisableMobilePushDeviceToken :one
UPDATE mobile_push_device_token
SET enabled = FALSE, updated_at = now()
WHERE user_id = $1
  AND workspace_id = $2
  AND provider = $3
  AND token = $4
RETURNING *;

-- name: DisableMobilePushDeviceTokenByID :exec
UPDATE mobile_push_device_token
SET enabled = FALSE, updated_at = now()
WHERE id = $1;

-- name: ListEnabledMobilePushTokensForInboxItem :many
SELECT t.*
FROM mobile_push_device_token t
LEFT JOIN notification_preference p
  ON p.workspace_id = t.workspace_id
 AND p.user_id = t.user_id
WHERE t.workspace_id = $1
  AND t.user_id = $2
  AND t.enabled = TRUE
  AND COALESCE(p.preferences->>'system_notifications', 'all') <> 'muted'
ORDER BY t.last_registered_at DESC;

-- name: CreateMobilePushDelivery :one
INSERT INTO mobile_push_delivery (
    inbox_item_id, device_token_id, provider, status,
    provider_message_id, error
) VALUES (
    $1, $2, $3, $4, sqlc.narg('provider_message_id'), sqlc.narg('error')
)
ON CONFLICT (inbox_item_id, device_token_id)
DO UPDATE SET
    status = EXCLUDED.status,
    provider_message_id = EXCLUDED.provider_message_id,
    error = EXCLUDED.error,
    updated_at = now()
RETURNING *;
