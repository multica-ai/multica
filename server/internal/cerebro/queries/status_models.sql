-- Cerebro workflow v2a: status models (FIR-1550).
-- Reusable workspace-level status pipelines + per-project selection.

-- name: ListCerebroStatusModels :many
SELECT id, workspace_id, name, description, statuses,
       workspace_default,
       created_by_id, created_by_type, created_at, updated_at
FROM cerebro_status_model
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: GetCerebroStatusModel :one
SELECT id, workspace_id, name, description, statuses,
       workspace_default,
       created_by_id, created_by_type, created_at, updated_at
FROM cerebro_status_model
WHERE id = $1;

-- name: GetWorkspaceDefaultStatusModel :one
-- FIR-2800: return the model marked as workspace default (at most one per
-- workspace, enforced by the partial unique index on workspace_id WHERE
-- workspace_default = true). Returns no row when no default is set.
SELECT id, workspace_id, name, description, statuses,
       workspace_default,
       created_by_id, created_by_type, created_at, updated_at
FROM cerebro_status_model
WHERE workspace_id = $1 AND workspace_default = true;

-- name: CreateCerebroStatusModel :one
INSERT INTO cerebro_status_model (
    workspace_id, name, description, statuses,
    created_by_id, created_by_type
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, workspace_id, name, description, statuses,
          workspace_default,
          created_by_id, created_by_type, created_at, updated_at;

-- name: UpdateCerebroStatusModel :one
UPDATE cerebro_status_model
SET name = $2,
    description = $3,
    statuses = $4,
    updated_at = now()
WHERE id = $1
RETURNING id, workspace_id, name, description, statuses,
          workspace_default,
          created_by_id, created_by_type, created_at, updated_at;

-- name: ClearWorkspaceDefaultStatusModel :exec
-- FIR-2800: clear the workspace_default flag from every model in the workspace.
-- Called before SetWorkspaceDefaultStatusModel to ensure at most one default.
UPDATE cerebro_status_model
SET workspace_default = false,
    updated_at = now()
WHERE workspace_id = $1 AND workspace_default = true;

-- name: SetWorkspaceDefaultStatusModel :one
-- FIR-2800: mark a specific model as the workspace default.
-- Always call ClearWorkspaceDefaultStatusModel first (in the same tx).
UPDATE cerebro_status_model
SET workspace_default = true,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, name, description, statuses,
          workspace_default,
          created_by_id, created_by_type, created_at, updated_at;

-- name: DeleteCerebroStatusModel :exec
DELETE FROM cerebro_status_model
WHERE id = $1;

-- name: CountProjectsUsingStatusModel :one
SELECT COUNT(*) AS project_count
FROM cerebro_project_status_model
WHERE status_model_id = $1;

-- name: GetProjectStatusModel :one
SELECT project_id, workspace_id, status_model_id,
       assigned_by_id, assigned_by_type, created_at, updated_at
FROM cerebro_project_status_model
WHERE project_id = $1;

-- name: ListProjectStatusModelsByWorkspace :many
SELECT project_id, workspace_id, status_model_id,
       assigned_by_id, assigned_by_type, created_at, updated_at
FROM cerebro_project_status_model
WHERE workspace_id = $1;

-- name: UpsertProjectStatusModel :one
INSERT INTO cerebro_project_status_model (
    project_id, workspace_id, status_model_id,
    assigned_by_id, assigned_by_type
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (project_id) DO UPDATE
    SET status_model_id = EXCLUDED.status_model_id,
        assigned_by_id = EXCLUDED.assigned_by_id,
        assigned_by_type = EXCLUDED.assigned_by_type,
        updated_at = now()
RETURNING project_id, workspace_id, status_model_id,
          assigned_by_id, assigned_by_type, created_at, updated_at;

-- name: DeleteProjectStatusModel :exec
DELETE FROM cerebro_project_status_model
WHERE project_id = $1;

-- v2b: per-issue custom status pin (FIR-1550).

-- name: GetCerebroIssueStatus :one
SELECT issue_id, workspace_id, status_model_id, custom_status_key,
       base_status, set_by_id, set_by_type, created_at, updated_at
FROM cerebro_issue_status
WHERE issue_id = $1;

-- name: ListCerebroIssueStatusesByModel :many
SELECT issue_id, workspace_id, status_model_id, custom_status_key,
       base_status, set_by_id, set_by_type, created_at, updated_at
FROM cerebro_issue_status
WHERE status_model_id = $1;

-- name: UpsertCerebroIssueStatus :one
INSERT INTO cerebro_issue_status (
    issue_id, workspace_id, status_model_id,
    custom_status_key, base_status,
    set_by_id, set_by_type
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (issue_id) DO UPDATE
    SET status_model_id = EXCLUDED.status_model_id,
        custom_status_key = EXCLUDED.custom_status_key,
        base_status = EXCLUDED.base_status,
        set_by_id = EXCLUDED.set_by_id,
        set_by_type = EXCLUDED.set_by_type,
        updated_at = now()
RETURNING issue_id, workspace_id, status_model_id, custom_status_key,
          base_status, set_by_id, set_by_type, created_at, updated_at;

-- name: DeleteCerebroIssueStatus :exec
DELETE FROM cerebro_issue_status
WHERE issue_id = $1;

-- name: DeleteCerebroIssueStatusesByModel :exec
DELETE FROM cerebro_issue_status
WHERE status_model_id = $1;
