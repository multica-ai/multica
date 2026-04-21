-- name: GetPipeline :one
SELECT * FROM pipeline
WHERE id = $1;

-- name: ListPipelinesByWorkspace :many
SELECT * FROM pipeline
WHERE workspace_id = $1
  AND (sqlc.arg(include_deleted)::bool OR deleted_at IS NULL)
ORDER BY created_at ASC;

-- name: ListPipelineColumns :many
SELECT * FROM pipeline_column
WHERE pipeline_id = $1
ORDER BY position ASC;
