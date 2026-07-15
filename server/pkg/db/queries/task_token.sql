-- name: CreateTaskToken :one
INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, space_id, space_ids, user_id, expires_at)
VALUES (
    sqlc.arg('token_hash'),
    sqlc.arg('task_id'),
    sqlc.arg('agent_id'),
    sqlc.arg('workspace_id'),
    sqlc.narg('space_id'),
    COALESCE(sqlc.arg('space_ids')::uuid[], ARRAY[]::uuid[]),
    sqlc.arg('user_id'),
    sqlc.arg('expires_at')
)
RETURNING *;

-- name: GetTaskTokenByHash :one
SELECT * FROM task_token
WHERE token_hash = $1 AND expires_at > now();

-- name: DeleteTaskTokensByTask :exec
DELETE FROM task_token WHERE task_id = $1;

-- name: DeleteExpiredTaskTokens :exec
DELETE FROM task_token WHERE expires_at <= now();
