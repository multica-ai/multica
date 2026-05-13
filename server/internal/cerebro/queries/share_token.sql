-- name: CreateShareToken :one
INSERT INTO cerebro_share_token (
    workspace_id, token, resource_kind, resource_id, action, persona_grant_id, created_by_id
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, workspace_id, token, resource_kind, resource_id, action, persona_grant_id, created_by_id, created_at, revoked_at;

-- name: GetShareTokenByToken :one
SELECT id, workspace_id, token, resource_kind, resource_id, action, persona_grant_id, created_by_id, created_at, revoked_at
FROM cerebro_share_token
WHERE token = $1 AND revoked_at IS NULL;

-- name: GetShareTokenByID :one
SELECT id, workspace_id, token, resource_kind, resource_id, action, persona_grant_id, created_by_id, created_at, revoked_at
FROM cerebro_share_token
WHERE id = $1;

-- name: ListShareTokensByResource :many
SELECT id, workspace_id, token, resource_kind, resource_id, action, persona_grant_id, created_by_id, created_at, revoked_at
FROM cerebro_share_token
WHERE workspace_id = $1 AND resource_kind = $2 AND resource_id = $3
ORDER BY created_at DESC;

-- name: RevokeShareToken :exec
UPDATE cerebro_share_token
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;
