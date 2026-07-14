-- =====================
-- Gitea Connection
-- =====================

-- name: ListGiteaConnectionsByWorkspace :many
SELECT * FROM gitea_connection
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: CreateGiteaConnection :one
-- Re-connecting the same instance (same base_url) refreshes the stored PAT and
-- account metadata rather than erroring, matching the GitHub upsert behaviour.
INSERT INTO gitea_connection (
    workspace_id, base_url, token_encrypted, account_login, account_avatar_url, connected_by_id
) VALUES (
    $1, $2, $3, $4, sqlc.narg('account_avatar_url'), sqlc.narg('connected_by_id')
)
ON CONFLICT (workspace_id, base_url) DO UPDATE SET
    token_encrypted = EXCLUDED.token_encrypted,
    account_login = EXCLUDED.account_login,
    account_avatar_url = EXCLUDED.account_avatar_url,
    connected_by_id = EXCLUDED.connected_by_id,
    updated_at = now()
RETURNING *;

-- name: DeleteGiteaConnection :exec
DELETE FROM gitea_connection WHERE id = $1 AND workspace_id = $2;
