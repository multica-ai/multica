-- name: CreateAgentToolPolicyRevision :one
INSERT INTO agent_tool_policy_revision (
    id, workspace_id, agent_id, revision, status, policy_digest,
    default_effect, rule_identities, actor_user_id, created_at
) VALUES (
    COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
    @workspace_id, @agent_id, @revision, @status, @policy_digest,
    'deny', @rule_identities, @actor_user_id, @created_at
)
RETURNING *;

-- name: GetAgentToolPolicyRevision :one
SELECT * FROM agent_tool_policy_revision
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND revision = @revision;

-- name: ListAgentToolPolicyRevisions :many
SELECT * FROM agent_tool_policy_revision
WHERE workspace_id = @workspace_id
  AND agent_id = @agent_id
  AND (
      sqlc.narg('before_revision')::bigint IS NULL
      OR revision < sqlc.narg('before_revision')::bigint
  )
ORDER BY revision DESC, id DESC
LIMIT @page_size::int;

-- name: DeleteAgentToolPolicyRevisionsForAgent :execrows
DELETE FROM agent_tool_policy_revision
WHERE workspace_id = @workspace_id AND agent_id = @agent_id;

-- name: DeleteAgentToolPolicyRevisionsForWorkspace :execrows
DELETE FROM agent_tool_policy_revision
WHERE workspace_id = @workspace_id;
