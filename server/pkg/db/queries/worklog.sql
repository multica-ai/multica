-- name: CreateWorklog :one
INSERT INTO worklog (workspace_id, author_type, author_id, duration_minutes, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateWorklogIssue :one
INSERT INTO worklog_issue (worklog_id, issue_id, workspace_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListWorklogsByIssue :many
SELECT
    w.id,
    wi.issue_id,
    w.workspace_id,
    w.author_type,
    w.author_id,
    w.duration_minutes,
    w.description,
    w.type,
    w.logged_at,
    w.created_at,
    w.updated_at
FROM worklog AS w
JOIN worklog_issue AS wi ON wi.worklog_id = w.id
WHERE wi.issue_id = $1
  AND wi.workspace_id = $2
ORDER BY w.logged_at DESC, w.created_at DESC;

-- name: GetWorklogByID :one
SELECT
    w.id,
    wi.issue_id,
    w.workspace_id,
    w.author_type,
    w.author_id,
    w.duration_minutes,
    w.description,
    w.type,
    w.logged_at,
    w.created_at,
    w.updated_at
FROM worklog AS w
JOIN worklog_issue AS wi ON wi.worklog_id = w.id
WHERE w.id = $1
  AND w.workspace_id = $2;

-- name: UpdateWorklog :one
UPDATE worklog
SET duration_minutes = $2,
    description = $3,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $4
RETURNING *;

-- name: DeleteWorklog :exec
DELETE FROM worklog
WHERE id = $1
  AND workspace_id = $2;