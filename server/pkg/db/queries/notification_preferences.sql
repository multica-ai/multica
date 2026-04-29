-- name: ListNotificationPreferences :many
SELECT * FROM notification_preference
WHERE workspace_id = $1 AND user_id = $2
ORDER BY notification_type;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preference (workspace_id, user_id, notification_type, enabled)
VALUES ($1, $2, $3, $4)
ON CONFLICT (workspace_id, user_id, notification_type)
DO UPDATE SET enabled = $4, updated_at = now()
RETURNING *;

-- name: GetDisabledNotificationTypes :many
SELECT notification_type FROM notification_preference
WHERE workspace_id = $1 AND user_id = $2 AND enabled = false;

-- name: ListNotificationPreferencesByType :many
SELECT user_id, enabled FROM notification_preference
WHERE workspace_id = $1 AND notification_type = $2;

-- name: DeleteNotificationPreferences :exec
DELETE FROM notification_preference
WHERE workspace_id = $1 AND user_id = $2;
