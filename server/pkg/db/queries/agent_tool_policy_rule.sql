-- name: CreateAgentToolPolicyRule :one
INSERT INTO agent_tool_policy_rule (
    id, workspace_id, agent_id, policy_id, transport_kind,
    server_key, tool_name, schema_digest, effect
) VALUES (
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
    @workspace_id, @agent_id, @policy_id, @transport_kind,
    @server_key, @tool_name, @schema_digest, @effect
)
RETURNING *;

-- name: ListAgentToolPolicyRules :many
SELECT * FROM agent_tool_policy_rule
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND policy_id = @policy_id
ORDER BY transport_kind ASC, server_key ASC, tool_name ASC, schema_digest ASC, id ASC;

-- name: GetAgentToolPolicyRuleExact :one
SELECT r.*
FROM agent_tool_policy_rule AS r
JOIN agent_tool_policy AS p
  ON p.workspace_id = r.workspace_id
 AND p.agent_id = r.agent_id
 AND p.id = r.policy_id
WHERE r.workspace_id = @workspace_id
  AND r.agent_id = @agent_id
  AND r.transport_kind = @transport_kind
  AND r.server_key = @server_key
  AND r.tool_name = @tool_name
  AND r.schema_digest = @schema_digest
  AND p.workspace_id = @workspace_id
  AND p.status = 'active'
  AND p.revision = @policy_revision;

-- name: DeleteAgentToolPolicyRulesForPolicy :execrows
DELETE FROM agent_tool_policy_rule
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND policy_id = @policy_id;

-- name: DeleteAgentToolPolicyRulesForAgent :execrows
DELETE FROM agent_tool_policy_rule
WHERE workspace_id = @workspace_id AND agent_id = @agent_id;

-- name: DeleteAgentToolPolicyRulesForWorkspace :execrows
DELETE FROM agent_tool_policy_rule
WHERE workspace_id = @workspace_id;
