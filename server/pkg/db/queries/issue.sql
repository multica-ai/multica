-- name: ListIssues :many
SELECT id, workspace_id, title, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, due_date, created_at, updated_at, number, project_id
FROM issue
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
ORDER BY position ASC, created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetIssue :one
SELECT * FROM issue
WHERE id = $1;

-- name: GetIssueInWorkspace :one
SELECT * FROM issue
WHERE id = $1 AND workspace_id = $2;

-- name: CreateIssue :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, due_date, number, project_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: GetIssueByNumber :one
SELECT * FROM issue
WHERE workspace_id = $1 AND number = $2;

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
    parent_issue_id = sqlc.narg('parent_issue_id'),
    project_id = sqlc.narg('project_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateIssueStatus :one
UPDATE issue SET
    status = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteIssue :exec
DELETE FROM issue WHERE id = $1;

-- name: ListOpenIssues :many
SELECT id, workspace_id, title, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, due_date, created_at, updated_at, number, project_id
FROM issue
WHERE workspace_id = $1
  AND status NOT IN ('done', 'cancelled')
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
ORDER BY position ASC, created_at DESC;

-- name: CountIssues :one
SELECT count(*) FROM issue
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'));

-- name: ListChildIssues :many
SELECT * FROM issue
WHERE parent_issue_id = $1
ORDER BY position ASC, created_at DESC;

-- name: CountCreatedIssueAssignees :many
-- Count assignees on issues created by a specific user.
SELECT
  assignee_type,
  assignee_id,
  COUNT(*)::bigint as frequency
FROM issue
WHERE workspace_id = $1
  AND creator_id = $2
  AND creator_type = 'member'
  AND assignee_type IS NOT NULL
  AND assignee_id IS NOT NULL
GROUP BY assignee_type, assignee_id;

-- name: ChildIssueProgress :many
SELECT parent_issue_id,
       COUNT(*)::bigint AS total,
       COUNT(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done
FROM issue
WHERE workspace_id = $1
  AND parent_issue_id IS NOT NULL
GROUP BY parent_issue_id;

-- SearchIssues: moved to handler (dynamic SQL for multi-word search support).
