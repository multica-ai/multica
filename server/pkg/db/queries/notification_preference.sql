-- name: GetNotificationPreference :one
SELECT * FROM notification_preference
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preference (workspace_id, user_id, preferences)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, user_id)
DO UPDATE SET preferences = $3, updated_at = now()
RETURNING *;

-- name: ListNotificationPreferencesByUsers :many
SELECT * FROM notification_preference
WHERE workspace_id = $1 AND user_id = ANY(sqlc.arg('user_ids')::uuid[]);
