-- name: GetAgentFullDelegation :one
SELECT * FROM agent_full_delegation WHERE agent_id = $1;

-- name: EnableAgentFullDelegation :exec
INSERT INTO agent_full_delegation (agent_id, grantor_user_id) VALUES ($1, $2)
ON CONFLICT (agent_id) DO UPDATE SET grantor_user_id = EXCLUDED.grantor_user_id, created_at = now();

-- name: RevokeAgentFullDelegation :exec
DELETE FROM agent_full_delegation WHERE agent_id = $1 AND grantor_user_id = $2;

-- name: DeleteAgentFullDelegationsForWorkspace :exec
DELETE FROM agent_full_delegation WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1);

-- name: ListUserFullDelegations :many
SELECT d.agent_id, d.grantor_user_id, d.created_at, a.name AS agent_name, a.workspace_id, w.name AS workspace_name
FROM agent_full_delegation d
JOIN agent a ON a.id = d.agent_id AND a.owner_id = d.grantor_user_id
JOIN workspace w ON w.id = a.workspace_id
WHERE d.grantor_user_id = $1 AND a.archived_at IS NULL AND a.kind = 'user'
ORDER BY d.created_at DESC;

-- name: AuthorizeFullAgentDelegation :one
SELECT d.grantor_user_id
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
JOIN agent_full_delegation d ON d.agent_id = a.id AND d.grantor_user_id = a.owner_id
JOIN chat_session c ON c.id = t.chat_session_id AND c.creator_id = d.grantor_user_id
JOIN member home_member ON home_member.user_id = d.grantor_user_id AND home_member.workspace_id = a.workspace_id
JOIN member target_member ON target_member.user_id = d.grantor_user_id AND target_member.workspace_id = sqlc.arg(target_workspace_id)
WHERE t.id = sqlc.arg(task_id) AND t.agent_id = sqlc.arg(agent_id)
  AND a.workspace_id = sqlc.arg(home_workspace_id) AND a.kind = 'user' AND a.archived_at IS NULL
  AND t.status = 'running' AND t.originator_source = 'direct_human'
  AND t.originator_user_id = d.grantor_user_id
  AND t.delegated_from_task_id IS NULL AND t.retry_of_task_id IS NULL
  AND home_member.role <> 'guest';
