-- name: GetAgentRuntimePreference :one
SELECT * FROM agent_runtime_preference
WHERE workspace_id = @workspace_id AND user_id = @user_id AND agent_id = @agent_id;

-- name: UpsertAgentRuntimePreference :one
INSERT INTO agent_runtime_preference (workspace_id, user_id, agent_id, runtime_id, model_mode, model, max_concurrent_tasks)
VALUES (@workspace_id, @user_id, @agent_id, @runtime_id, COALESCE(NULLIF(@model_mode::text, ''), 'inherit'), @model, sqlc.narg(max_concurrent_tasks))
ON CONFLICT (workspace_id, user_id, agent_id)
DO UPDATE SET runtime_id = EXCLUDED.runtime_id, model_mode = EXCLUDED.model_mode, model = EXCLUDED.model, max_concurrent_tasks = EXCLUDED.max_concurrent_tasks, updated_at = now()
RETURNING *;

-- name: DeleteAgentRuntimePreference :exec
DELETE FROM agent_runtime_preference
WHERE workspace_id = @workspace_id AND user_id = @user_id AND agent_id = @agent_id;

-- name: ListAgentRuntimePreferences :many
SELECT * FROM agent_runtime_preference
WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: DeleteAgentRuntimePreferencesByWorkspace :exec
DELETE FROM agent_runtime_preference WHERE workspace_id = @workspace_id;

-- name: DeleteAgentRuntimePreferencesByMember :exec
DELETE FROM agent_runtime_preference WHERE workspace_id = @workspace_id AND user_id = @user_id;

-- name: ListRuntimeRoutingCandidates :many
SELECT a.id AS agent_id, a.runtime_id, r.owner_id AS runtime_owner_id, r.provider, a.model
FROM agent a
JOIN agent_runtime r ON r.id = a.runtime_id AND r.workspace_id = a.workspace_id
WHERE a.workspace_id = @workspace_id AND a.archived_at IS NULL;

-- name: IsTaskRuntimeAllowed :one
SELECT task_runtime_allowed(@agent_id::uuid, @runtime_id::uuid, sqlc.narg(runtime_routing)::jsonb)::boolean AS allowed;
