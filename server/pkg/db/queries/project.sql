-- name: CreateProject :one
INSERT INTO project (
    workspace_id, name, description, status, icon, color,
    lead_type, lead_id, start_date, target_date, sort_order
) VALUES (
    $1, $2, $3, $4, $5, $6,
    sqlc.narg('lead_type'), sqlc.narg('lead_id'),
    sqlc.narg('start_date'), sqlc.narg('target_date'), $7
) RETURNING *;

-- name: GetProject :one
SELECT * FROM project
WHERE id = $1 AND workspace_id = $2;

-- name: ListProjects :many
SELECT * FROM project
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY sort_order ASC, created_at DESC;

-- name: UpdateProject :one
UPDATE project SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    icon = COALESCE(sqlc.narg('icon'), icon),
    color = COALESCE(sqlc.narg('color'), color),
    lead_type = sqlc.narg('lead_type'),
    lead_id = sqlc.narg('lead_id'),
    start_date = sqlc.narg('start_date'),
    target_date = sqlc.narg('target_date'),
    sort_order = COALESCE(sqlc.narg('sort_order'), sort_order),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM project WHERE id = $1 AND workspace_id = $2;

-- name: GetProjectProgress :one
SELECT
    COUNT(*)::int AS total,
    COUNT(*) FILTER (WHERE status IN ('done', 'cancelled'))::int AS completed
FROM issue
WHERE project_id = $1 AND workspace_id = $2;

-- name: ListProjectsProgress :many
SELECT
    project_id,
    COUNT(*)::int AS total,
    COUNT(*) FILTER (WHERE status IN ('done', 'cancelled'))::int AS completed
FROM issue
WHERE workspace_id = $1 AND project_id IS NOT NULL
GROUP BY project_id;
