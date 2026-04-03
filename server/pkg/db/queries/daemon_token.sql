-- name: CreateDaemonToken :one
INSERT INTO daemon_token (id, token_hash, workspace_id, daemon_id, expires_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDaemonTokenByHash :one
SELECT * FROM daemon_token
WHERE token_hash = ? AND expires_at > datetime('now');

-- name: DeleteDaemonTokensByWorkspaceAndDaemon :exec
DELETE FROM daemon_token
WHERE workspace_id = ? AND daemon_id = ?;

-- name: DeleteExpiredDaemonTokens :exec
DELETE FROM daemon_token
WHERE expires_at <= datetime('now');
