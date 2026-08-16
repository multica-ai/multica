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

-- name: CreateAgentToolActionLog :exec
INSERT INTO agent_action_log (
    agent_id, issue_id, task_id, message_seq, tool_name,
    args_summary, result_summary, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (task_id, message_seq) WHERE task_id IS NOT NULL AND message_seq IS NOT NULL
DO NOTHING;

-- name: ListRecentAgentActions :many
SELECT * FROM agent_action_log
ORDER BY created_at DESC
LIMIT $1;
