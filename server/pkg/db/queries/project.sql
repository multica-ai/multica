-- name: ListProjects :many
SELECT * FROM project
WHERE project.workspace_id = $1
  AND (sqlc.narg('team_id')::uuid IS NULL OR EXISTS (
    SELECT 1 FROM project_team pt
    WHERE pt.project_id = project.id
      AND pt.workspace_id = project.workspace_id
      AND pt.team_id = sqlc.narg('team_id')::uuid
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

-- name: ListProjectTeams :many
SELECT wt.* FROM workspace_team wt
JOIN project_team pt ON pt.team_id = wt.id AND pt.workspace_id = wt.workspace_id
WHERE pt.workspace_id = $1
  AND pt.project_id = $2
ORDER BY wt.is_default DESC, wt.name ASC, wt.created_at ASC;

-- name: ProjectHasTeam :one
SELECT EXISTS (
  SELECT 1 FROM project_team
  WHERE workspace_id = $1
    AND project_id = $2
    AND team_id = $3
)::boolean;

-- name: ListProjectTeamsByProjects :many
SELECT pt.project_id, wt.id, wt.workspace_id, wt.name, wt.key, wt.description,
       wt.icon, wt.issue_counter, wt.is_default, wt.archived_at, wt.archived_by,
       wt.created_by, wt.created_at, wt.updated_at
FROM project_team pt
JOIN workspace_team wt ON wt.id = pt.team_id AND wt.workspace_id = pt.workspace_id
WHERE pt.workspace_id = $1
  AND pt.project_id = ANY(sqlc.arg('project_ids')::uuid[])
ORDER BY pt.project_id, wt.is_default DESC, wt.name ASC, wt.created_at ASC;

-- name: AddProjectTeam :exec
INSERT INTO project_team (workspace_id, project_id, team_id)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, team_id) DO NOTHING;

-- name: ReplaceProjectTeams :exec
WITH deleted AS (
  DELETE FROM project_team
  WHERE workspace_id = sqlc.arg('workspace_id')
    AND project_id = sqlc.arg('project_id')
    AND NOT (team_id = ANY(sqlc.arg('team_ids')::uuid[]))
)
INSERT INTO project_team (workspace_id, project_id, team_id)
SELECT sqlc.arg('workspace_id'), sqlc.arg('project_id'), unnest(sqlc.arg('team_ids')::uuid[])
ON CONFLICT (project_id, team_id) DO NOTHING;

-- name: CountProjectIssuesByTeam :one
SELECT count(*) FROM issue
WHERE workspace_id = $1
  AND project_id = $2
  AND team_id = $3;

-- name: CountActiveProjectAutopilotsByTeam :one
SELECT count(*) FROM autopilot
WHERE workspace_id = $1
  AND project_id = $2
  AND team_id = $3
  AND status <> 'archived';
