-- name: GetAgentToolPolicy :one
SELECT * FROM agent_tool_policy
WHERE workspace_id = @workspace_id AND agent_id = @agent_id;

-- name: GetAgentToolPolicyByID :one
SELECT * FROM agent_tool_policy
WHERE workspace_id = @workspace_id AND id = @id;

-- name: LockAgentToolPolicy :one
SELECT * FROM agent_tool_policy
WHERE workspace_id = @workspace_id AND agent_id = @agent_id
FOR UPDATE;

-- name: CreateAgentToolPolicy :one
INSERT INTO agent_tool_policy (
    id, workspace_id, agent_id, revision, status, policy_digest,
    default_effect, created_by_user_id, updated_by_user_id
) VALUES (
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
    @workspace_id, @agent_id, @revision, @status, @policy_digest,
    'deny', @created_by_user_id, @updated_by_user_id
)
RETURNING *;

-- name: UpdateAgentToolPolicyRevision :one
UPDATE agent_tool_policy
SET revision = @next_revision,
    status = @status,
    policy_digest = @policy_digest,
    default_effect = 'deny',
    updated_by_user_id = @updated_by_user_id,
    updated_at = @updated_at
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND revision = @expected_revision
RETURNING *;

-- name: DeleteAgentToolPolicyForAgent :execrows
DELETE FROM agent_tool_policy
WHERE workspace_id = @workspace_id AND agent_id = @agent_id;

-- name: DeleteAgentToolPoliciesForWorkspace :execrows
DELETE FROM agent_tool_policy
WHERE workspace_id = @workspace_id;

-- name: CountActiveAgentToolPolicies :one
SELECT count(*) FROM agent_tool_policy
WHERE workspace_id = @workspace_id AND status = 'active';
