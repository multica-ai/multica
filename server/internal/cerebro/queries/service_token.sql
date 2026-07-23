-- name: CreateCerebroServiceToken :one
WITH inserted AS (
    INSERT INTO cerebro_service_token (
        workspace_id, name, token_hash, token_prefix, scopes, expires_at, created_by
    ) VALUES (
        sqlc.arg(workspace_id), sqlc.arg(name), sqlc.arg(token_hash),
        sqlc.arg(token_prefix), sqlc.arg(scopes), sqlc.arg(expires_at),
        sqlc.arg(created_by)
    )
    RETURNING id, workspace_id, name, token_hash, token_prefix, scopes,
              expires_at, last_used_at, revoked, created_by, created_at
),
audited AS (
    INSERT INTO cerebro_service_token_audit (
        service_token_id, workspace_id, event, actor_user_id, detail
    )
    SELECT id, workspace_id, 'issued', created_by, sqlc.arg(audit_detail)
    FROM inserted
    RETURNING service_token_id
)
SELECT i.id, i.workspace_id, i.name, i.token_hash, i.token_prefix, i.scopes,
       i.expires_at, i.last_used_at, i.revoked, i.created_by, i.created_at
FROM inserted i
JOIN audited a ON a.service_token_id = i.id;

-- name: GetCerebroServiceTokenByHash :one
-- Auth path: only a live token resolves. A revoked or expired token returns
-- no rows, so the middleware fails closed exactly like the PAT lookup.
SELECT id, workspace_id, name, token_hash, token_prefix, scopes, expires_at, last_used_at, revoked, created_by, created_at
FROM cerebro_service_token
WHERE token_hash = $1
  AND revoked = FALSE
  AND expires_at > now();

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
-- never revoke another workspace's token. Revocation and its audit event are
-- one database statement: either both persist or neither does.
WITH updated AS (
    UPDATE cerebro_service_token
    SET revoked = TRUE
    WHERE cerebro_service_token.id = sqlc.arg(token_id)
      AND cerebro_service_token.workspace_id = sqlc.arg(workspace_id)
    RETURNING id, workspace_id, name, token_hash, token_prefix, scopes,
              expires_at, last_used_at, revoked, created_by, created_at
),
audited AS (
    INSERT INTO cerebro_service_token_audit (
        service_token_id, workspace_id, event, actor_user_id, detail
    )
    SELECT id, workspace_id, 'revoked', sqlc.arg(actor_user_id), NULL
    FROM updated
    RETURNING service_token_id
)
SELECT u.id, u.workspace_id, u.name, u.token_hash, u.token_prefix, u.scopes,
       u.expires_at, u.last_used_at, u.revoked, u.created_by, u.created_at
FROM updated u
JOIN audited a ON a.service_token_id = u.id;

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
