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

-- name: ProjectHasStagingEnv :one
-- Returns TRUE when the project has at least one deploy_environment
-- row with kind='staging'. Used by the release stage flow to decide
-- whether merging → in_staging (project has staging) or
-- merging → promoting (project ships direct-to-prod). Existence-
-- based: the staging env may or may not have a deploy_workflow_filename
-- configured; if the row exists at all, the release goes through
-- staging stages.
SELECT EXISTS (
    SELECT 1 FROM deploy_environment
    WHERE project_id = $1 AND kind = 'staging'
)::bool AS has_staging;

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
--
-- `deploy_workflow_filename` is the per-env GitHub Actions workflow
-- the auto-detect poller watches for this environment. Nullable —
-- when null, the poller falls back to the workspace-level setting
-- (`workspace.ship_hub_deploy_workflow_<kind>`). When the workspace
-- has multiple projects, each project's env can override the default
-- with its own repo's workflow filename.
INSERT INTO deploy_environment (
    workspace_id, project_id, kind, name, target_branch, target_url,
    auto_promote, deploy_workflow_filename, auto_deploy
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (project_id, kind) DO UPDATE SET
    name                     = EXCLUDED.name,
    target_branch            = EXCLUDED.target_branch,
    target_url               = EXCLUDED.target_url,
    auto_promote             = EXCLUDED.auto_promote,
    deploy_workflow_filename = EXCLUDED.deploy_workflow_filename,
    auto_deploy              = EXCLUDED.auto_deploy,
    updated_at               = now()
RETURNING *;

-- name: UpdateDeployEnvironment :one
-- PATCH path. narg fields are nullable for "leave alone" semantics. Every
-- update bumps updated_at unconditionally.
--
-- `deploy_workflow_filename` uses sqlc.narg (not COALESCE) so passing
-- an explicit empty string clears the override (poller falls back to
-- workspace setting); passing NULL leaves the current value alone.
UPDATE deploy_environment SET
    name                     = COALESCE(sqlc.narg('name'), name),
    target_branch            = COALESCE(sqlc.narg('target_branch'), target_branch),
    target_url               = sqlc.narg('target_url'),
    auto_promote             = COALESCE(sqlc.narg('auto_promote'), auto_promote),
    deploy_workflow_filename = COALESCE(sqlc.narg('deploy_workflow_filename'), deploy_workflow_filename),
    auto_deploy              = COALESCE(sqlc.narg('auto_deploy'), auto_deploy),
    updated_at               = now()
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

-- name: GetDeployEnvironmentByRepoAndName :one
-- Webhook-driven deploy ingestion needs to resolve "which env does this
-- GitHub deployment.created event refer to" by (repo_url, environment
-- name). The repo_url comes from the deployment's repository.html_url;
-- env name from deployment.environment. We join via project_resource so
-- only environments whose project has the matching github_repo resource
-- match — keeps two workspaces with the same env name from colliding.
SELECT de.* FROM deploy_environment de
JOIN project_resource pr ON pr.project_id = de.project_id
WHERE de.workspace_id = sqlc.arg('workspace_id')
  AND pr.resource_type = 'github_repo'
  AND pr.resource_ref->>'url' = sqlc.arg('repo_url')::text
  AND de.name = sqlc.arg('env_name')::text
LIMIT 1;

-- name: GetDeployByEnvAndSHA :one
-- Looks up the most recent deploy row for (environment, sha). Used by
-- the deployment_status webhook handler to find the row to update.
SELECT * FROM deploy
WHERE environment_id = $1 AND sha = $2
ORDER BY triggered_at DESC
LIMIT 1;

-- name: UpdateDeployStatus :one
-- Webhook-driven status transition. Caller supplies the timestamps to
-- match the GitHub event semantics (in_progress → started_at; success/
-- failure → completed_at).
UPDATE deploy SET
    status        = $2,
    started_at    = COALESCE(sqlc.narg('started_at'), started_at),
    completed_at  = COALESCE(sqlc.narg('completed_at'), completed_at),
    log_url       = COALESCE(sqlc.narg('log_url'), log_url),
    error_message = COALESCE(sqlc.narg('error_message'), error_message)
WHERE id = $1
RETURNING *;

-- name: ListDeploysByEnvironmentBefore :many
-- Phase 5 time-machine — list every deploy attempt against an env up to
-- timestamp $2. Used to reconstruct "what SHA was running on this env
-- as of that moment". Ordered triggered_at DESC so the caller can take
-- the first succeeded row as the active SHA at $2.
SELECT * FROM deploy
WHERE environment_id = $1
  AND triggered_at <= sqlc.arg('at')::timestamptz
ORDER BY triggered_at DESC;

-- name: CountWorkspaceDeploysIn24h :one
-- Phase 5 — backs the ambient sidebar's "in production last 24h" segment.
SELECT count(*)::bigint AS count
FROM deploy d
JOIN deploy_environment de ON de.id = d.environment_id
WHERE d.workspace_id = $1
  AND de.kind = 'production'
  AND d.status = 'succeeded'
  AND d.completed_at IS NOT NULL
  AND d.completed_at > NOW() - INTERVAL '24 hours';
