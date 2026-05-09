-- name: GetSystemSetting :one
SELECT value FROM system_settings
WHERE key = $1;

-- name: ListSystemSettings :many
SELECT * FROM system_settings;

-- name: UpdateSystemSetting :exec
UPDATE system_settings
SET value = $2, updated_at = NOW()
WHERE key = $1;
