-- name: GetOrCreateSystemSetting :one
INSERT INTO system_setting (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE
SET value = system_setting.value
RETURNING value;
