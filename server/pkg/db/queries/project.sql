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
-- CEREBRO-PATCH(list-projects-grant-access): FIR-2125 add OR-clause so members with
-- a cerebro_project_grant (direct or inherited via project_nesting) also see the
-- project. The old model (project.access / project_member) stays additive; both
-- models coexist during the rollout of the cerebro_collections flag.
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
    OR EXISTS (
      WITH RECURSIVE ancestors AS (
        SELECT p.id AS anc_id, 0 AS depth
        UNION ALL
        SELECT pn.parent_project_id, a.depth + 1
        FROM project_nesting pn
        JOIN ancestors a ON pn.project_id = a.anc_id
        WHERE pn.parent_project_id IS NOT NULL AND a.depth < 3
      )
      SELECT 1 FROM ancestors anc
      JOIN cerebro_project_grant g ON g.project_id = anc.anc_id AND g.workspace_id = $1
      WHERE g.grantee_type = 'workspace'
         OR (g.grantee_type = 'member' AND g.grantee_id = sqlc.arg('user_id')::uuid)
         OR EXISTS (
           SELECT 1 FROM cerebro_group_member cgm
           WHERE cgm.user_id = sqlc.arg('user_id')::uuid
             AND cgm.group_id = g.grantee_id
             AND g.grantee_type = 'group'
         )
    )
  )
  AND (sqlc.narg('status')::text IS NULL OR p.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR p.priority = sqlc.narg('priority'))
ORDER BY p.created_at DESC;

-- name: GetProjectByRepoURL :one
SELECT * FROM project
WHERE workspace_id = $1 AND repo_url = $2;

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
