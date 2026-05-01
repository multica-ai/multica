-- Fork-specific: per-user preferences storage.
-- Kept in a dedicated file so upstream-merges don't conflict on user.sql.

-- name: UpdateUserPreferences :one
-- Replaces the user's preferences JSONB blob wholesale. Caller is responsible
-- for merging existing values when doing partial updates.
UPDATE "user"
SET preferences = @preferences::jsonb,
    updated_at = now()
WHERE id = @user_id
RETURNING *;

-- name: GetUserPreferences :one
SELECT preferences FROM "user" WHERE id = $1;
