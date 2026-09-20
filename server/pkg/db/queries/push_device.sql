-- name: UpsertPushDevice :one
INSERT INTO push_device (user_id, expo_push_token, platform)
VALUES ($1, $2, $3)
ON CONFLICT (expo_push_token) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    platform = EXCLUDED.platform,
    updated_at = now()
RETURNING *;

-- name: DeletePushDevice :exec
DELETE FROM push_device
WHERE user_id = $1 AND expo_push_token = $2;

-- name: DeletePushDeviceByToken :exec
DELETE FROM push_device
WHERE expo_push_token = $1;

-- name: ListPushTargetsForNotification :many
SELECT d.expo_push_token, w.slug AS workspace_slug
FROM push_device d
JOIN workspace w ON w.id = sqlc.arg('workspace_id')
LEFT JOIN notification_preference p
    ON p.workspace_id = w.id AND p.user_id = d.user_id
WHERE d.user_id = sqlc.arg('user_id')
  AND COALESCE(p.preferences ->> 'system_notifications', 'all') <> 'muted';
