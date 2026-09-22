-- name: HasTasksByIssue :one
-- Any task, including a terminal task, proves webhook ownership transferred.
SELECT EXISTS (SELECT 1 FROM agent_task_queue WHERE issue_id = $1);
