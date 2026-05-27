-- Cerebro workflow v2a: status models (FIR-1550).
-- Reusable workspace-level status pipelines + per-project selection.

-- name: ListCerebroStatusModels :many
SELECT id, workspace_id, name, description, statuses,
       created_by_id, created_by_type, created_at, updated_at
FROM cerebro_status_model
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: GetCerebroStatusModel :one
SELECT id, workspace_id, name, description, statuses,
       created_by_id, created_by_type, created_at, updated_at
FROM cerebro_status_model
WHERE id = $1;

-- name: CreateCerebroStatusModel :one
INSERT INTO cerebro_status_model (
    workspace_id, name, description, statuses,
    created_by_id, created_by_type
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, workspace_id, name, description, statuses,
          created_by_id, created_by_type, created_at, updated_at;

-- name: UpdateCerebroStatusModel :one
UPDATE cerebro_status_model
SET name = $2,
    description = $3,
    statuses = $4,
    updated_at = now()
WHERE id = $1
RETURNING id, workspace_id, name, description, statuses,
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
