-- name: CreateDaemonToken :one
INSERT INTO multica_daemon_token (token_hash, workspace_id, daemon_id, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetDaemonTokenByHash :one
SELECT * FROM multica_daemon_token
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteDaemonTokensByWorkspaceAndDaemons :many
-- Deletes every multica_daemon_token row matching the (workspace_id, daemon_id)
-- pairs implied by `daemon_ids`. Used by the multica_member-revocation flow to
-- nuke tokens for all runtimes a leaving multica_member owned in one shot.
-- Returns token_hash so the caller can invalidate auth.DaemonTokenCache
-- before the 10-minute TTL expires — without that invalidate, a daemon
-- can keep using its stale token until cache eviction even though the
-- DB row is gone.
DELETE FROM multica_daemon_token
WHERE workspace_id = @workspace_id
  AND daemon_id = ANY(@daemon_ids::text[])
RETURNING token_hash;

-- name: DeleteExpiredDaemonTokens :exec
DELETE FROM multica_daemon_token
WHERE expires_at <= now();
