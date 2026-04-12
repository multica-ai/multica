-- name: CreateSandboxConfig :one
INSERT INTO workspace_sandbox_config (
    workspace_id, name, provider, provider_api_key, ai_gateway_api_key, git_pat, template_id, metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetSandboxConfigByID :one
SELECT * FROM workspace_sandbox_config WHERE id = $1;

-- name: GetSandboxConfig :one
SELECT * FROM workspace_sandbox_config WHERE workspace_id = $1 LIMIT 1;

-- name: ListSandboxConfigsByWorkspace :many
SELECT * FROM workspace_sandbox_config WHERE workspace_id = $1 ORDER BY created_at;

-- name: UpdateSandboxConfig :one
UPDATE workspace_sandbox_config SET
    name = $2,
    provider = $3,
    provider_api_key = $4,
    ai_gateway_api_key = $5,
    git_pat = $6,
    template_id = $7,
    metadata = $8,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteSandboxConfigByID :exec
DELETE FROM workspace_sandbox_config WHERE id = $1;

-- name: DeleteSandboxConfig :exec
DELETE FROM workspace_sandbox_config WHERE workspace_id = $1;

-- name: ListSandboxConfigs :many
SELECT workspace_id, provider, template_id, created_at, updated_at
FROM workspace_sandbox_config;
