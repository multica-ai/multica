-- name: GetNotificationPreference :one
SELECT * FROM notification_preference WHERE user_id = $1;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preference (user_id, ntfy_url, ntfy_token, disabled_types, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (user_id) DO UPDATE SET
    ntfy_url       = EXCLUDED.ntfy_url,
    ntfy_token     = EXCLUDED.ntfy_token,
    disabled_types = EXCLUDED.disabled_types,
    updated_at     = now()
RETURNING *;
