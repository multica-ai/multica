-- name: CreateSetupToken :one
INSERT INTO setup_token (
    user_id, workspace_id, token_hash, token_prefix, expires_at
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ConsumeSetupToken :one
-- Atomic compare-and-swap: concurrent/replayed exchanges can update at most one
-- row. The caller creates the long-lived PAT in the same transaction, so a PAT
-- insertion or commit failure rolls this consumption back as well.
UPDATE setup_token
SET redeemed_at = now()
WHERE token_hash = $1
  AND redeemed_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: GetSetupTokenForUser :one
SELECT * FROM setup_token
WHERE id = $1
  AND workspace_id = $2
  AND user_id = $3;

-- name: GetRedeemedSetupTokenForDaemon :one
SELECT * FROM setup_token
WHERE id = $1
  AND workspace_id = $2
  AND user_id = $3
  AND redeemed_at IS NOT NULL;

-- name: MarkSetupTokenDaemonConnected :one
UPDATE setup_token
SET daemon_connected_at = COALESCE(daemon_connected_at, now()),
    daemon_id = sqlc.arg(daemon_id),
    runtime_count = GREATEST(runtime_count, sqlc.arg(runtime_count)::integer)
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
  AND user_id = sqlc.arg(user_id)
  AND redeemed_at IS NOT NULL
RETURNING *;

-- name: DeleteExpiredSetupTokens :exec
-- Keep redeemed rows for one day after command expiry so a slow daemon can
-- still report its final status; everything older is no longer useful to UI.
DELETE FROM setup_token
WHERE expires_at < now() - INTERVAL '1 day';

-- name: DeleteSetupTokensByWorkspaceAndUser :exec
DELETE FROM setup_token
WHERE workspace_id = $1 AND user_id = $2;

-- name: DeleteSetupTokensByWorkspace :exec
DELETE FROM setup_token
WHERE workspace_id = $1;
