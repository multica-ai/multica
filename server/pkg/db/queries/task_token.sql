-- name: CreateTaskToken :one
INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at, id)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()))
RETURNING *;

-- name: GetTaskTokenByHash :one
-- A mat_ token remains valid only while its task is active and its
-- Agent/runtime/owner binding is still authorized.
-- Live routing checks revoke access when membership or invocation authority changes.
SELECT tt.*
FROM task_token tt
JOIN agent_task_queue atq ON atq.id = tt.task_id
JOIN agent a ON a.id = atq.agent_id AND a.id = tt.agent_id
JOIN agent_runtime r ON r.id = atq.runtime_id
WHERE tt.token_hash = $1
  AND tt.expires_at > now()
  AND tt.workspace_id = r.workspace_id
  AND (
      (atq.runtime_routing IS NULL AND tt.user_id = r.owner_id)
      OR (
          atq.runtime_routing IS NOT NULL
          AND tt.user_id::text = atq.runtime_routing->>'execution_user_id'
      )
  )
  AND task_runtime_allowed(atq.agent_id, atq.runtime_id, atq.runtime_routing)
  AND a.workspace_id = r.workspace_id
  AND a.archived_at IS NULL
  AND atq.status IN ('dispatched', 'waiting_local_directory', 'running');

-- name: DeleteTaskTokensByTask :exec
DELETE FROM task_token WHERE task_id = $1;

-- name: DeleteExpiredTaskTokens :exec
DELETE FROM task_token WHERE expires_at <= now();
