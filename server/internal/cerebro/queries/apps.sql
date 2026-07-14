-- FIR-3172 mini-app catalog and workflow runtime.

-- name: ListCerebroApps :many
SELECT * FROM cerebro_app
WHERE workspace_id = $1
ORDER BY folder, name;

-- name: GetCerebroAppInWorkspace :one
SELECT * FROM cerebro_app
WHERE id = $1 AND workspace_id = $2;

-- name: CreateCerebroApp :one
INSERT INTO cerebro_app (workspace_id, slug, name, description, icon, folder, owner_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateCerebroAppPublishedVersion :one
UPDATE cerebro_app
SET current_version = $2, status = 'published', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateCerebroAppVersion :one
INSERT INTO cerebro_app_version (app_id, version, content_snapshot, release_notes, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCerebroAppVersion :one
SELECT * FROM cerebro_app_version
WHERE app_id = $1 AND version = $2;

-- name: ListCerebroAppVersions :many
SELECT * FROM cerebro_app_version
WHERE app_id = $1
ORDER BY created_at DESC;

-- name: CreateCerebroAppChangeRequest :one
INSERT INTO cerebro_app_change_request (
    app_id, base_version, proposed_version, proposed_snapshot,
    release_notes, proposed_by
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpsertCerebroAppKV :one
INSERT INTO cerebro_app_kv (app_id, key, value, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (app_id, key) DO UPDATE
SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: GetCerebroAppKV :one
SELECT * FROM cerebro_app_kv WHERE app_id = $1 AND key = $2;

-- name: DeleteCerebroAppKV :exec
DELETE FROM cerebro_app_kv WHERE app_id = $1 AND key = $2;

-- name: CreateCerebroAppWorkflowDef :one
INSERT INTO cerebro_app_workflow_def (
    workspace_id, app_id, name, definition, version, enabled, owner_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListCerebroAppWorkflowDefs :many
SELECT * FROM cerebro_app_workflow_def
WHERE workspace_id = $1
ORDER BY updated_at DESC;

-- name: SetCerebroAppWorkflowEnabled :one
UPDATE cerebro_app_workflow_def
SET enabled = $2, updated_at = now()
WHERE id = $1 AND workspace_id = $3
RETURNING *;

-- name: CreateCerebroAppWorkflowRun :one
INSERT INTO cerebro_app_workflow_run (
    workflow_id, workflow_version, identity_envelope, trigger_payload
)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListCerebroAppWorkflowRuns :many
SELECT r.* FROM cerebro_app_workflow_run r
JOIN cerebro_app_workflow_def d ON d.id = r.workflow_id
WHERE d.workspace_id = $1
ORDER BY r.created_at DESC
LIMIT $2;
