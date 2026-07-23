-- name: CreateCerebroServiceToken :one
INSERT INTO cerebro_service_token (
    workspace_id, name, token_hash, token_prefix, scopes, expires_at, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING id, workspace_id, name, token_hash, token_prefix, scopes, expires_at, last_used_at, revoked, created_by, created_at;

-- name: GetCerebroServiceTokenByHash :one
-- Auth path: only a live token resolves. A revoked or expired token returns
-- no rows, so the middleware fails closed exactly like the PAT lookup.
SELECT id, workspace_id, name, token_hash, token_prefix, scopes, expires_at, last_used_at, revoked, created_by, created_at
FROM cerebro_service_token
WHERE token_hash = $1
  AND revoked = FALSE
  AND (expires_at IS NULL OR expires_at > now());

-- name: TouchCerebroServiceToken :exec
UPDATE cerebro_service_token
SET last_used_at = now()
WHERE id = $1;

-- name: ListCerebroServiceTokensByWorkspace :many
SELECT id, workspace_id, name, token_hash, token_prefix, scopes, expires_at, last_used_at, revoked, created_by, created_at
FROM cerebro_service_token
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: RevokeCerebroServiceToken :one
-- Idempotent revoke scoped to the workspace so one workspace's admin can
-- never revoke another workspace's token. Returns the row so the caller can
-- audit and drop any cache entry.
UPDATE cerebro_service_token
SET revoked = TRUE
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, name, token_hash, token_prefix, scopes, expires_at, last_used_at, revoked, created_by, created_at;

-- name: AppendCerebroServiceTokenAudit :exec
INSERT INTO cerebro_service_token_audit (
    service_token_id, workspace_id, event, actor_user_id, detail
) VALUES (
    $1, $2, $3, $4, $5
);

-- name: ListCerebroServiceTokenAuditByWorkspace :many
SELECT id, service_token_id, workspace_id, event, actor_user_id, detail, created_at
FROM cerebro_service_token_audit
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2;
