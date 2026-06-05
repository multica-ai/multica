-- name: GetAgentRuntimeBinding :one
SELECT * FROM agent_runtime_binding
WHERE agent_id = $1 AND user_id = $2;

-- name: UpsertAgentRuntimeBinding :one
INSERT INTO agent_runtime_binding (agent_id, user_id, runtime_id)
VALUES ($1, $2, $3)
ON CONFLICT (agent_id, user_id)
DO UPDATE SET runtime_id = EXCLUDED.runtime_id, updated_at = now()
RETURNING *;

-- name: DeleteAgentRuntimeBinding :exec
DELETE FROM agent_runtime_binding
WHERE agent_id = $1 AND user_id = $2;
