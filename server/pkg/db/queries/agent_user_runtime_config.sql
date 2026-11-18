-- name: GetAgentUserRuntimeConfig :one
SELECT * FROM agent_user_runtime_config
WHERE agent_id = $1 AND user_id = $2;

-- name: LockAgentUserRuntimeConfigsForUpdate :many
-- Bulk personal runtime migration locks existing overrides after locking the
-- corresponding agent rows. Existing single-agent upserts then serialize on
-- these rows. Writers also lock the agent before changing its overrides.
SELECT * FROM agent_user_runtime_config
WHERE agent_id = ANY(@agent_ids::uuid[])
  AND user_id = @user_id
ORDER BY agent_id
FOR UPDATE;

-- name: AgentHasUserRuntimeBinding :one
-- True when any user override currently points this agent at runtime_id.
-- Claim SQL and the Go-side runtime_mismatch fence consult this as one of the
-- smart-runtime (share_runtime=false) bindings; originator-owned CLI-match
-- machines are a separate current binding and do not require this row.
SELECT EXISTS (
    SELECT 1
    FROM agent_user_runtime_config
    WHERE agent_id = $1
      AND runtime_id = $2
);

-- name: AgentHasUserRuntimeBindingForUser :one
-- Private-runtime owner check: the override must belong to the runtime owner,
-- not merely exist for some other member.
SELECT EXISTS (
    SELECT 1
    FROM agent_user_runtime_config
    WHERE agent_id = $1
      AND runtime_id = $2
      AND user_id = $3
);

-- name: ClearAgentUserRuntimeConfigsByRuntime :execrows
-- Runtime deletion locks the parent agent_runtime row before this update. The
-- runtime lock used by override writers serializes with this cleanup.
UPDATE agent_user_runtime_config
SET runtime_id = NULL, updated_at = now()
WHERE runtime_id = $1;

-- name: UpsertAgentUserRuntimeConfig :one
INSERT INTO agent_user_runtime_config (
    agent_id, user_id, runtime_id, model, thinking_level, service_tier, runtime_config,
    max_concurrent_tasks
) VALUES (
    $1, $2, $3, $4, $5, $6, COALESCE(sqlc.narg('runtime_config'), '{}'::jsonb), $7
)
ON CONFLICT (agent_id, user_id) DO UPDATE SET
    runtime_id = EXCLUDED.runtime_id,
    model = EXCLUDED.model,
    thinking_level = EXCLUDED.thinking_level,
    service_tier = EXCLUDED.service_tier,
    runtime_config = EXCLUDED.runtime_config,
    max_concurrent_tasks = EXCLUDED.max_concurrent_tasks,
    updated_at = now()
RETURNING *;

-- name: ClearAgentUserRuntimeConfigOverride :one
INSERT INTO agent_user_runtime_config (
    agent_id, user_id, runtime_id, model, thinking_level, service_tier, runtime_config,
    max_concurrent_tasks
) VALUES (
    $1, $2, NULL, '', '', '', '{}'::jsonb, 0
)
ON CONFLICT (agent_id, user_id) DO UPDATE SET
    runtime_id = NULL,
    model = '',
    thinking_level = '',
    service_tier = '',
    runtime_config = '{}'::jsonb,
    max_concurrent_tasks = 0,
    updated_at = now()
RETURNING *;

-- name: MarkAgentUserRuntimeConfigNoticeSeen :one
INSERT INTO agent_user_runtime_config (
    agent_id, user_id, notice_seen_at
) VALUES (
    $1, $2, now()
)
ON CONFLICT (agent_id, user_id) DO UPDATE SET
    notice_seen_at = now(),
    updated_at = now()
RETURNING *;

-- name: DeleteAgentUserRuntimeConfigsForMember :exec
DELETE FROM agent_user_runtime_config c USING agent a
WHERE c.agent_id = a.id AND a.workspace_id = @workspace_id AND c.user_id = @user_id;

-- name: DeleteAgentUserRuntimeConfigsByWorkspace :exec
DELETE FROM agent_user_runtime_config c USING agent a
WHERE c.agent_id = a.id AND a.workspace_id = @workspace_id;
