-- name: InsertTaskToolConfirmation :exec
INSERT INTO task_tool_confirmation (task_id, workspace_id, issue_id, call_id, tool_name, mcp_server_name, input)
VALUES ($1, $2, $3, $4, $5, sqlc.narg('mcp_server_name'), $6)
ON CONFLICT (task_id, call_id) DO NOTHING;

-- name: ListTaskToolConfirmations :many
SELECT * FROM task_tool_confirmation
WHERE task_id = $1
ORDER BY created_at, call_id;

-- name: ListPendingTaskToolConfirmations :many
SELECT * FROM task_tool_confirmation
WHERE task_id = ANY(sqlc.arg('task_ids')::uuid[]) AND status = 'pending'
ORDER BY created_at, call_id;

-- name: DecideTaskToolConfirmation :one
-- Only a pending call on a still-running task can be decided; the caller maps
-- no row to a conflict.
UPDATE task_tool_confirmation c
SET status = sqlc.arg('status'), deny_message = sqlc.narg('deny_message'),
    decided_by = sqlc.arg('decided_by'), decided_at = now()
WHERE c.task_id = sqlc.arg('task_id') AND c.call_id = sqlc.arg('call_id') AND c.status = 'pending'
  AND EXISTS (SELECT 1 FROM agent_task_queue t WHERE t.id = c.task_id AND t.status = 'running')
RETURNING c.*;
