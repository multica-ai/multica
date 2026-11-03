-- Pipelines (spec 3.1). Every query filters by workspace_id. Pipelines are few
-- per workspace, so the lists are not indexed by workspace.

-- name: LockPipelineInstall :exec
-- Serializes the default pipeline install within one workspace, so two installs
-- cannot both create the pipeline. Released at commit.
SELECT pg_advisory_xact_lock(hashtext('pipeline-install'), hashtext(sqlc.arg('workspace_id')::uuid::text));

-- name: ListPipelines :many
SELECT * FROM pipeline
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY is_default DESC, created_at, id;

-- name: GetPipelineInWorkspace :one
SELECT * FROM pipeline
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: GetDefaultPipeline :one
SELECT * FROM pipeline
WHERE workspace_id = sqlc.arg('workspace_id')::uuid AND is_default
ORDER BY created_at, id
LIMIT 1;

-- name: CreatePipeline :one
INSERT INTO pipeline (workspace_id, name, workflow_id, project_id, stages, is_default)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('name')::text,
    sqlc.arg('workflow_id')::uuid,
    sqlc.arg('project_id')::uuid,
    sqlc.arg('stages')::jsonb,
    sqlc.arg('is_default')::boolean
)
RETURNING *;

-- name: DeleteWorkspacePipelines :exec
-- Workspace teardown: pipelines have no foreign keys to cascade.
DELETE FROM pipeline WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: UpdatePipelineStages :one
-- Fills the owners the install could not resolve before.
UPDATE pipeline SET stages = sqlc.arg('stages')::jsonb, updated_at = now()
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;
