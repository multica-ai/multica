-- name: ListIssues :many
SELECT id, workspace_id, title, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, due_date, created_at, updated_at, number, project_id
FROM issue
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
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
ORDER BY position ASC, created_at DESC;

-- name: CountIssues :one
SELECT count(*) FROM issue
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'));

-- name: ListChildIssues :many
SELECT * FROM issue
WHERE parent_issue_id = $1
ORDER BY position ASC, created_at DESC;

-- name: SearchIssues :many
SELECT i.*,
  COUNT(*) OVER() AS total_count,
  CASE
    WHEN i.title LIKE '%' || @query || '%' THEN 'title'
    WHEN COALESCE(i.description, '') LIKE '%' || @query || '%' THEN 'description'
    ELSE 'comment'
  END AS match_source,
  CASE
    WHEN i.title LIKE '%' || @query || '%' THEN ''
    WHEN COALESCE(i.description, '') LIKE '%' || @query || '%' THEN ''
    ELSE COALESCE(
      (SELECT c.content FROM comment c
       WHERE c.issue_id = i.id AND c.content LIKE '%' || @query || '%'
       ORDER BY c.created_at DESC LIMIT 1),
      ''
    )
  END AS matched_comment_content
FROM issue i
WHERE i.workspace_id = @workspace_id
  AND (
    i.title LIKE '%' || @query || '%'
    OR COALESCE(i.description, '') LIKE '%' || @query || '%'
    OR EXISTS (
      SELECT 1 FROM comment c
      WHERE c.issue_id = i.id AND c.content LIKE '%' || @query || '%'
    )
  )
  AND (@include_closed::boolean OR i.status NOT IN ('done', 'cancelled'))
ORDER BY
  CASE
    WHEN i.title LIKE '%' || @query || '%' THEN 0
    WHEN COALESCE(i.description, '') LIKE '%' || @query || '%' THEN 1
    ELSE 2
  END,
  i.updated_at DESC
LIMIT @search_limit OFFSET @search_offset;
