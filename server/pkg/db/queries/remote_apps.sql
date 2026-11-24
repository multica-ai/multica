-- =====================
-- Remote MCP app OAuth clients
-- =====================

-- name: GetRemoteAppOAuthClient :one
SELECT * FROM remote_app_oauth_client
WHERE app_slug = $1 AND redirect_uri = $2;

-- name: GetRemoteAppOAuthClientByID :one
SELECT * FROM remote_app_oauth_client
WHERE id = $1;

-- name: InsertRemoteAppOAuthClient :one
-- A concurrent first connect may have registered already; the caller reads
-- that row when this returns no rows.
INSERT INTO remote_app_oauth_client (
    app_slug, redirect_uri, authorization_endpoint, token_endpoint, scope,
    client_id, client_secret, token_endpoint_auth_method
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (app_slug, redirect_uri) DO NOTHING
RETURNING *;

-- name: DeleteRemoteAppOAuthClient :exec
-- The authorization server no longer knows the client; the next connect
-- registers a new one.
DELETE FROM remote_app_oauth_client WHERE id = $1;

-- =====================
-- Remote MCP app connections
-- =====================

-- name: UpsertRemoteAppConnection :one
INSERT INTO remote_app_connection (
    app_slug, scope, owner_id, connected_by, oauth_client_id,
    access_token, refresh_token, expires_at, status, workspace_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, 'active', $9
)
ON CONFLICT (scope, owner_id, app_slug, workspace_id) DO UPDATE SET
    connected_by    = EXCLUDED.connected_by,
    oauth_client_id = EXCLUDED.oauth_client_id,
    access_token    = EXCLUDED.access_token,
    refresh_token   = EXCLUDED.refresh_token,
    expires_at      = EXCLUDED.expires_at,
    status          = 'active',
    connected_at    = now(),
    updated_at      = now()
RETURNING *;

-- name: ListRemoteAppConnectionsForOwner :many
-- Every account the owner still sees in one workspace: active and expired,
-- not disconnected. Accounts made before they belonged to a workspace count
-- in every workspace.
SELECT * FROM remote_app_connection
WHERE scope = $1 AND owner_id = $2 AND status <> 'revoked'
  AND (workspace_id = sqlc.arg('workspace_id')::uuid OR workspace_id IS NULL)
ORDER BY (workspace_id IS NULL), connected_at DESC;

-- name: GetActiveRemoteAppConnection :one
SELECT * FROM remote_app_connection
WHERE app_slug = $1 AND scope = $2 AND owner_id = $3 AND status = 'active'
  AND (workspace_id = sqlc.arg('workspace_id')::uuid OR workspace_id IS NULL)
ORDER BY (workspace_id IS NULL)
LIMIT 1;

-- name: GetRemoteAppConnectionForOwner :one
-- Owner-scoped, so an account cannot be changed by guessing its id.
SELECT * FROM remote_app_connection
WHERE id = $1 AND scope = $2 AND owner_id = $3;

-- name: GetRemoteAppConnection :one
SELECT * FROM remote_app_connection
WHERE id = $1;

-- name: LockRemoteAppConnection :one
-- Serializes token refresh: a refresh token is single-use, so two refreshes
-- of the same account must not race.
SELECT * FROM remote_app_connection
WHERE id = $1
FOR UPDATE;

-- name: UpdateRemoteAppConnectionTokens :exec
UPDATE remote_app_connection
SET access_token = $2, refresh_token = $3, expires_at = $4, updated_at = now()
WHERE id = $1;

-- name: SetRemoteAppConnectionStatus :exec
-- Leaving active drops the tokens: an expired account needs a new sign-in and
-- a revoked one must not keep a usable credential.
UPDATE remote_app_connection
SET status = $2,
    access_token = CASE WHEN $2::text = 'active' THEN access_token END,
    refresh_token = CASE WHEN $2::text = 'active' THEN refresh_token END,
    updated_at = now()
WHERE id = $1;

-- name: TouchRemoteAppConnection :exec
UPDATE remote_app_connection
SET last_used_at = now()
WHERE id = $1;
