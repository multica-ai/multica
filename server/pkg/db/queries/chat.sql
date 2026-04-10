-- name: CreateChatSession :one
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetChatSession :one
SELECT * FROM chat_session
WHERE id = $1;

-- name: GetChatSessionInWorkspace :one
SELECT * FROM chat_session
WHERE id = $1 AND workspace_id = $2;

-- name: ListChatSessionsByCreator :many
SELECT * FROM chat_session
WHERE workspace_id = $1 AND creator_id = $2 AND status = 'active'
ORDER BY updated_at DESC;

-- name: ListAllChatSessionsByCreator :many
SELECT * FROM chat_session
WHERE workspace_id = $1 AND creator_id = $2
ORDER BY updated_at DESC;

-- name: UpdateChatSessionTitle :one
UPDATE chat_session SET title = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateChatSessionSession :exec
UPDATE chat_session SET session_id = $2, work_dir = $3, updated_at = now()
WHERE id = $1;

-- name: ArchiveChatSession :exec
UPDATE chat_session SET status = 'archived', updated_at = now()
WHERE id = $1;

-- name: TouchChatSession :exec
UPDATE chat_session SET updated_at = now()
WHERE id = $1;

-- name: CreateChatMessage :one
INSERT INTO chat_message (chat_session_id, role, content, task_id)
VALUES ($1, $2, $3, sqlc.narg(task_id))
RETURNING *;

-- name: ListChatMessages :many
SELECT * FROM chat_message
WHERE chat_session_id = $1
ORDER BY created_at ASC;

-- name: GetChatMessage :one
SELECT * FROM chat_message
WHERE id = $1;

-- name: CreateChatTask :one
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, chat_session_id)
VALUES ($1, $2, NULL, 'queued', $3, $4)
RETURNING *;

-- name: GetLastChatTaskSession :one
SELECT session_id, work_dir FROM agent_task_queue
WHERE chat_session_id = $1 AND status = 'completed' AND session_id IS NOT NULL
ORDER BY completed_at DESC
LIMIT 1;
