-- name: CreateTaskToken :one
INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTaskTokenByHash :one
SELECT * FROM task_token
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteTaskTokensByTask :exec
DELETE FROM task_token WHERE task_id = $1;

-- name: DeleteExpiredTaskTokens :exec
DELETE FROM task_token WHERE expires_at <= now();
