-- name: CreateAgentActionLog :one
INSERT INTO agent_action_log (
    agent_id, issue_id, tool_name, args_summary, result_summary, status
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListAgentActionsByAgent :many
SELECT * FROM agent_action_log
WHERE agent_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: ListRecentAgentActions :many
SELECT * FROM agent_action_log
ORDER BY created_at DESC
LIMIT $1;
