-- name: ListChannelConnections :many
SELECT * FROM channel_connection
ORDER BY provider ASC, created_at ASC;

-- name: GetChannelConnection :one
SELECT * FROM channel_connection
WHERE id = $1;

-- name: BootstrapChannelConnection :exec
INSERT INTO channel_connection (
    id, provider, display_name, enabled, is_default, config, secret_config, status
) VALUES (
    $1, $2, $3, true, $4, $5, $6, 'configured'
)
ON CONFLICT (id) DO NOTHING;

-- name: CreateChannelConnection :one
INSERT INTO channel_connection (
    id, provider, display_name, enabled, is_default, config, secret_config, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    CASE WHEN $4 THEN 'configured' ELSE 'disabled' END
)
RETURNING *;

-- name: UpdateChannelConnection :one
UPDATE channel_connection SET
    display_name = $2,
    enabled = $3,
    is_default = $4,
    config = $5,
    secret_config = $6,
    status = CASE WHEN $3 THEN 'configured' ELSE 'disabled' END,
    last_error = NULL,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteChannelConnection :exec
DELETE FROM channel_connection
WHERE id = $1;

-- name: UpdateChannelConnectionStatus :exec
UPDATE channel_connection SET
    status = $2,
    last_error = $3,
    updated_at = now()
WHERE id = $1;
