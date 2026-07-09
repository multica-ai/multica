-- name: ListProjects :many
SELECT * FROM project
WHERE project.workspace_id = $1
  AND (sqlc.narg('space_id')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM project_space pt
    WHERE pt.project_id = project.id
      AND pt.workspace_id = project.workspace_id
      AND pt.space_id = sqlc.narg('space_id')::uuid
  ))
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
    workspace_id, title, description, icon, status,
    lead_type, lead_id, priority
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
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

-- name: ListProjectSpaces :many
SELECT wt.* FROM workspace_space wt
JOIN project_space pt ON pt.space_id = wt.id AND pt.workspace_id = wt.workspace_id
WHERE pt.workspace_id = $1
  AND pt.project_id = $2
ORDER BY wt.name ASC, wt.created_at ASC;

-- name: ListProjectSpacesByProjects :many
SELECT pt.project_id, wt.id, wt.workspace_id, wt.name, wt.key, wt.description,
       wt.icon, wt.issue_counter, wt.archived_at, wt.archived_by,
       wt.created_by, wt.created_at, wt.updated_at
FROM project_space pt
JOIN workspace_space wt ON wt.id = pt.space_id AND wt.workspace_id = pt.workspace_id
WHERE pt.workspace_id = $1
  AND pt.project_id = ANY(sqlc.arg('project_ids')::uuid[])
ORDER BY pt.project_id, wt.name ASC, wt.created_at ASC;

-- name: AddProjectSpace :exec
INSERT INTO project_space (workspace_id, project_id, space_id)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, space_id) DO NOTHING;

-- name: CountProjectIssuesBySpace :many
-- Guards removing a Space from a project's space set: which of the Spaces
-- about to be removed still have issues filed under this project, and how
-- many. UpdateProject rejects the removal (or requires a reassignment
-- target via space_reassignments) for any Space this returns a row for.
-- workspace_id is a SQL-layer tenant guard (defense-in-depth, matching
-- DeleteProject/DeleteIssue) even though callers only ever pass a
-- project_id already resolved within their own workspace.
SELECT space_id, count(*)::bigint AS issue_count
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
  AND space_id = ANY(sqlc.arg('space_ids')::uuid[])
GROUP BY space_id;

-- name: ReplaceProjectSpaces :exec
WITH deleted AS (
  DELETE FROM project_space
  WHERE workspace_id = sqlc.arg('workspace_id')
    AND project_id = sqlc.arg('project_id')
    AND NOT (space_id = ANY(sqlc.arg('space_ids')::uuid[]))
)
INSERT INTO project_space (workspace_id, project_id, space_id)
SELECT sqlc.arg('workspace_id'), sqlc.arg('project_id'), unnest(sqlc.arg('space_ids')::uuid[])
ON CONFLICT (project_id, space_id) DO NOTHING;