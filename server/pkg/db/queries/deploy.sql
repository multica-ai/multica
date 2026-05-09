-- name: ListDeployEnvironmentsByProject :many
-- Ordered so 'staging' surfaces before 'production' in the Ship Hub UI —
-- matches the natural left-to-right deployment progression.
SELECT * FROM deploy_environment
WHERE project_id = $1
ORDER BY kind ASC, created_at ASC;

-- name: ListDeployEnvironmentsByWorkspace :many
SELECT * FROM deploy_environment
WHERE workspace_id = $1
ORDER BY project_id, kind ASC;

-- name: GetDeployEnvironment :one
SELECT * FROM deploy_environment WHERE id = $1;

-- name: GetDeployEnvironmentInWorkspace :one
SELECT * FROM deploy_environment
WHERE id = $1 AND workspace_id = $2;

-- name: CountDeployEnvironmentsForProjects :many
-- Used by the Ship Hub project list to render an "envs configured" hint
-- alongside the open-PR count.
SELECT project_id, count(*)::bigint AS env_count
FROM deploy_environment
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;

-- name: UpsertDeployEnvironment :one
-- Setup endpoint: creating or reconfiguring (project, kind). The unique
-- constraint on (project_id, kind) makes this safe to call repeatedly.
INSERT INTO deploy_environment (
    workspace_id, project_id, kind, name, target_branch, target_url,
    auto_promote
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (project_id, kind) DO UPDATE SET
    name          = EXCLUDED.name,
    target_branch = EXCLUDED.target_branch,
    target_url    = EXCLUDED.target_url,
    auto_promote  = EXCLUDED.auto_promote,
    updated_at    = now()
RETURNING *;

-- name: UpdateDeployEnvironment :one
-- PATCH path. narg fields are nullable for "leave alone" semantics. Every
-- update bumps updated_at unconditionally.
UPDATE deploy_environment SET
    name          = COALESCE(sqlc.narg('name'), name),
    target_branch = COALESCE(sqlc.narg('target_branch'), target_branch),
    target_url    = sqlc.narg('target_url'),
    auto_promote  = COALESCE(sqlc.narg('auto_promote'), auto_promote),
    updated_at    = now()
WHERE id = $1
RETURNING *;

-- name: UpdateDeployEnvironmentCurrent :one
-- Called by the periodic reconciler / manual sync when a deploy completes
-- successfully — bumps the "what's running" answer to a single column read.
UPDATE deploy_environment SET
    current_sha         = $2,
    current_deployed_at = $3,
    updated_at          = now()
WHERE id = $1
RETURNING *;

-- name: DeleteDeployEnvironment :exec
DELETE FROM deploy_environment WHERE id = $1;

-- name: ListRecentDeploysByEnvironment :many
SELECT * FROM deploy
WHERE environment_id = $1
ORDER BY triggered_at DESC
LIMIT $2;

-- name: GetDeploy :one
SELECT * FROM deploy WHERE id = $1;

-- name: InsertDeploy :one
INSERT INTO deploy (
    workspace_id, environment_id, ref, sha, status, triggered_by,
    started_at, completed_at, log_url, error_message
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;
