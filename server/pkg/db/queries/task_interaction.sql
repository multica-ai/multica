-- name: LockTaskInteraction :one
SELECT id, status, interaction FROM agent_task_queue WHERE id = $1 FOR UPDATE;

-- name: SaveTaskInteraction :exec
UPDATE agent_task_queue SET interaction = $2 WHERE id = $1;

-- name: TaskInteractionCredentialDeadline :one
SELECT COALESCE(min(expires_at), now())::timestamptz AS deadline
FROM task_token WHERE task_id = $1;

-- name: LockIssueForInteractiveFollowup :one
SELECT * FROM issue WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: MarkTaskInteractiveFollowup :exec
UPDATE agent_task_queue SET context = COALESCE(context, '{}'::jsonb) || '{"require_session_resume":true}'::jsonb WHERE id = $1;
