-- =====================
-- Forgejo Connection
-- =====================

-- name: ListForgejoConnectionsByWorkspace :many
SELECT * FROM forgejo_connection
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetForgejoConnectionByID :one
SELECT * FROM forgejo_connection
WHERE id = $1;

-- name: GetForgejoConnectionForWorkspace :one
SELECT * FROM forgejo_connection
WHERE id = $1 AND workspace_id = $2;

-- name: UpsertForgejoConnection :one
-- Reconnecting the same instance rotates the stored token/secret and identity
-- in place rather than creating a duplicate row.
INSERT INTO forgejo_connection (
    workspace_id, instance_url, account_login,
    access_token_encrypted, webhook_secret_encrypted, connected_by_id
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('connected_by_id')
)
ON CONFLICT (workspace_id, instance_url) DO UPDATE SET
    account_login            = EXCLUDED.account_login,
    access_token_encrypted   = EXCLUDED.access_token_encrypted,
    webhook_secret_encrypted = EXCLUDED.webhook_secret_encrypted,
    connected_by_id          = EXCLUDED.connected_by_id,
    updated_at               = now()
RETURNING *;

-- name: DeleteForgejoConnection :exec
DELETE FROM forgejo_connection WHERE id = $1 AND workspace_id = $2;
