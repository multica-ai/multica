-- name: CreateTaskToken :one
INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at, id)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()))
RETURNING *;

-- name: GetTaskTokenByHash :one
-- A token authenticates only while its task is live. Terminal task rows never
-- return to a live status (retries are new rows), so a token whose task is
-- completed, failed or cancelled is dead even when no path deleted it: the
-- bulk failure sweeps (runtime offline, stale tasks, daemon recovery) never
-- delete it, and the per-task deletes are best-effort (GitHub #11378).
SELECT tt.* FROM task_token tt
JOIN agent_task_queue q ON q.id = tt.task_id
WHERE tt.token_hash = $1
  AND tt.expires_at > now()
  AND q.status NOT IN ('completed', 'failed', 'cancelled');

-- name: DeleteTaskTokensByTask :exec
DELETE FROM task_token WHERE task_id = $1;

-- name: DeleteExpiredTaskTokens :exec
DELETE FROM task_token WHERE expires_at <= now();
