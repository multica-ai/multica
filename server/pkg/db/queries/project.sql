-- name: ListProjects :many
SELECT * FROM project
WHERE project.workspace_id = $1
  AND (
    EXISTS (
      SELECT 1 FROM workspace_space wt
      WHERE wt.id = project.space_id
        AND wt.workspace_id = project.workspace_id
        AND wt.visibility = 'open'
    )
    OR EXISTS (
      SELECT 1 FROM workspace_space_member sm
      WHERE sm.space_id = project.space_id
        AND sm.user_id = sqlc.arg('viewer_user_id')::uuid
    )
    OR EXISTS (
      SELECT 1 FROM member wm
      WHERE wm.workspace_id = project.workspace_id
        AND wm.user_id = sqlc.arg('viewer_user_id')::uuid
        AND wm.role IN ('owner', 'admin')
    )
  )
  AND (sqlc.narg('space_id')::uuid IS NULL OR project.space_id = sqlc.narg('space_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR project.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR project.priority = sqlc.narg('priority'))
ORDER BY created_at DESC;

-- name: GetProject :one
SELECT * FROM project
WHERE id = $1;

-- name: GetProjectInWorkspace :one
SELECT * FROM project
WHERE id = $1 AND workspace_id = $2;

-- name: CreateProject :one
INSERT INTO project (
    workspace_id, space_id, title, description, icon, status,
    lead_type, lead_id, priority, start_date, due_date
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = sqlc.narg('description'),
    icon = sqlc.narg('icon'),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = sqlc.narg('lead_type'),
    lead_id = sqlc.narg('lead_id'),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
DELETE FROM project WHERE id = $1 AND workspace_id = $2;

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = $1;

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done_count
FROM issue
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;
