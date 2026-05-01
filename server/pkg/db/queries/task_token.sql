-- name: CreateTaskToken :exec
INSERT INTO task_token (token_hash, task_id, issue_id, agent_id, workspace_id, scope, expires_at)
VALUES ($1, $2, sqlc.narg(issue_id), $3, $4, $5, $6);

-- name: GetTaskTokenByHash :one
SELECT * FROM task_token
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: RevokeTaskTokensForTask :exec
UPDATE task_token
SET revoked_at = now()
WHERE task_id = $1 AND revoked_at IS NULL;

-- name: DeleteExpiredTaskTokens :exec
DELETE FROM task_token
WHERE expires_at < now() - INTERVAL '24 hours';
