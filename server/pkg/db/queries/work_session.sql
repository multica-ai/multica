-- CEREBRO-PATCH(sqlc-work-session): cerebro modification of upstream file
-- name: CreateWorkSession :one
INSERT INTO work_session (workspace_id, issue_id, user_id, session_type, work_dir, branch)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetWorkSession :one
SELECT * FROM work_session WHERE id = $1;

-- name: CompleteWorkSession :one
UPDATE work_session
SET status = 'completed',
    summary = $2,
    session_id = $3,
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: FailWorkSession :one
UPDATE work_session
SET status = 'failed',
    summary = $2,
    completed_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateWorkSessionName :one
UPDATE work_session SET name = $2 WHERE id = $1
RETURNING *;

-- name: ListWorkSessionsByIssue :many
SELECT * FROM work_session
WHERE issue_id = $1
ORDER BY created_at DESC;

-- name: CreateWorkSessionMessage :one
INSERT INTO task_message (work_session_id, seq, type, tool, content, input, output)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ResumeWorkSession :one
UPDATE work_session
SET status = 'active',
    completed_at = NULL
WHERE id = $1
RETURNING *;

-- name: GetActiveWorkSessionForUser :one
SELECT * FROM work_session
WHERE user_id = $1 AND workspace_id = $2 AND status = 'active'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetLastWorkSessionSeq :one
SELECT COALESCE(MAX(seq), 0)::integer AS max_seq FROM task_message
WHERE work_session_id = $1;

-- name: ListWorkSessionMessages :many
SELECT * FROM task_message
WHERE work_session_id = $1
ORDER BY seq ASC;
