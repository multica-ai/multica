-- name: ListIssues :many
SELECT * FROM issue
WHERE workspace_id = ?
  AND (sqlc.narg('status') IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority') IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id') IS NULL OR assignee_id = sqlc.narg('assignee_id'))
ORDER BY position ASC, created_at DESC
LIMIT ? OFFSET ?;

-- name: GetIssue :one
SELECT * FROM issue
WHERE id = ?;

-- name: GetIssueInWorkspace :one
SELECT * FROM issue
WHERE id = ? AND workspace_id = ?;

-- name: CreateIssue :one
INSERT INTO issue (
    id, workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, due_date, number
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
) RETURNING *;

-- name: GetIssueByNumber :one
SELECT * FROM issue
WHERE workspace_id = ? AND number = ?;

-- name: UpdateIssue :one
UPDATE issue SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = COALESCE(sqlc.narg('position'), position),
    due_date = sqlc.narg('due_date'),
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: UpdateIssueStatus :one
UPDATE issue SET
    status = ?,
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: DeleteIssue :exec
DELETE FROM issue WHERE id = ?;
