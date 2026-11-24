-- name: GetNotificationPreference :one
SELECT * FROM notification_preference
WHERE workspace_id = $1 AND user_id = $2;

-- name: UpsertNotificationPreference :one
INSERT INTO notification_preference (workspace_id, user_id, preferences)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, user_id)
DO UPDATE SET preferences = $3, updated_at = now()
RETURNING *;

-- name: PatchNotificationPreference :one
INSERT INTO notification_preference (workspace_id, user_id, preferences)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, user_id)
DO UPDATE SET
    preferences = notification_preference.preferences || EXCLUDED.preferences,
    updated_at = now()
RETURNING *;

-- name: ResolveAutoArchivePrompt :exec
-- Answers the one-time auto-archive question in every workspace where the
-- user still has it pending: merges their choice and drops the marker.
UPDATE notification_preference
SET preferences = (preferences - 'auto_archive_prompt') || sqlc.arg('preferences')::jsonb,
    updated_at = now()
WHERE user_id = sqlc.arg('user_id')
  AND preferences ->> 'auto_archive_prompt' IS NOT NULL;

-- name: ListNotificationPreferencesByUsers :many
SELECT * FROM notification_preference
WHERE workspace_id = $1 AND user_id = ANY(sqlc.arg('user_ids')::uuid[]);
