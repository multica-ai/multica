-- CEREBRO-PATCH(sqlc-project): cerebro modification of upstream file
-- name: ListProjects :many
SELECT * FROM project
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
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
    lead_type, lead_id, priority, color
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = sqlc.narg('description'),
    icon = sqlc.narg('icon'),
    color = sqlc.narg('color'),
    repo_url = sqlc.narg('repo_url'),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = sqlc.narg('lead_type'),
    lead_id = sqlc.narg('lead_id'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateProjectAccess :one
UPDATE project SET access = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListProjectsAccessibleToUser :many
-- Workspace-bound list filtered by access rules:
--   * project.access = 'workspace'         → visible to every member
--   * project.access = 'restricted' AND
--     (workspace admin/owner OR explicit project_member row)
-- CEREBRO-PATCH(list-projects-group-access): JEH-1009 add OR-clause so non-admin
-- members also see projects granted via any of their cerebro_group memberships.
SELECT p.* FROM project p
WHERE p.workspace_id = $1
  AND (
    p.access = 'workspace'
    OR sqlc.arg('is_admin')::boolean
    OR EXISTS (
      SELECT 1 FROM project_member pm
      WHERE pm.project_id = p.id AND pm.user_id = sqlc.arg('user_id')::uuid
    )
    OR EXISTS (SELECT 1 FROM cerebro_project_group_member pgm JOIN cerebro_group_member gm ON gm.group_id = pgm.group_id WHERE pgm.project_id = p.id AND gm.user_id = sqlc.arg('user_id')::uuid)
  )
  AND (sqlc.narg('status')::text IS NULL OR p.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR p.priority = sqlc.narg('priority'))
ORDER BY p.created_at DESC;

-- name: GetProjectByRepoURL :one
SELECT * FROM project
WHERE workspace_id = $1 AND repo_url = $2;

-- name: DeleteProject :exec
DELETE FROM project WHERE id = $1;

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
