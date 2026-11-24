-- name: ListAgentWorkspaceGrants :many
SELECT workspace_id FROM agent_workspace_grant WHERE agent_id = $1 ORDER BY created_at;

-- name: AuthorizeTaskWorkspaceGrant :one
SELECT a.owner_id, t.originator_user_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
JOIN agent_workspace_grant g ON g.agent_id = a.id AND g.workspace_id = sqlc.arg(target_workspace_id)
JOIN member owner_member ON owner_member.user_id = a.owner_id AND owner_member.workspace_id = g.workspace_id
JOIN member origin_member ON origin_member.user_id = t.originator_user_id AND origin_member.workspace_id = g.workspace_id
WHERE t.id = sqlc.arg(task_id) AND t.agent_id = sqlc.arg(agent_id)
  AND a.workspace_id = sqlc.arg(home_workspace_id) AND a.kind = 'user' AND a.archived_at IS NULL
  AND t.status = 'running' AND t.originator_source = 'direct_human'
  AND t.delegated_from_task_id IS NULL AND t.retry_of_task_id IS NULL
  AND owner_member.role IN ('owner', 'admin', 'member')
  AND origin_member.role IN ('owner', 'admin', 'member');

-- name: AddAgentWorkspaceGrant :exec
INSERT INTO agent_workspace_grant (agent_id, workspace_id, created_by)
VALUES ($1, $2, $3) ON CONFLICT (agent_id, workspace_id) DO NOTHING;

-- name: RemoveAgentWorkspaceGrant :exec
DELETE FROM agent_workspace_grant WHERE agent_id = $1 AND workspace_id = $2;

-- name: DeleteAgentWorkspaceGrantsForWorkspace :exec
DELETE FROM agent_workspace_grant
WHERE agent_workspace_grant.workspace_id = $1
   OR agent_workspace_grant.agent_id IN (SELECT agent.id FROM agent WHERE agent.workspace_id = $1);
