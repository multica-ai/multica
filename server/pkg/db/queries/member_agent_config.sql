-- name: GetMemberAgentConfig :one
SELECT * FROM member_agent_config
WHERE member_id = $1 AND workspace_id = $2;

-- name: UpsertMemberAgentConfig :one
INSERT INTO member_agent_config (member_id, workspace_id, config)
VALUES ($1, $2, $3)
ON CONFLICT (member_id, workspace_id) DO UPDATE SET
    config = EXCLUDED.config,
    updated_at = now()
RETURNING *;

-- name: DeleteMemberAgentConfig :exec
DELETE FROM member_agent_config
WHERE member_id = $1 AND workspace_id = $2;

-- name: GetMemberAgentConfigByOwner :one
-- 通过 agent.owner_id 查找该用户在该 workspace 的个人 agent config
SELECT mac.* FROM member_agent_config mac
JOIN member m ON mac.member_id = m.id
WHERE m.user_id = $1 AND mac.workspace_id = $2;
