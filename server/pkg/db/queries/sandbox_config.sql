-- name: UpsertSandboxConfig :one
INSERT INTO workspace_sandbox_config (
    workspace_id, provider, provider_api_key, ai_gateway_api_key, git_pat, template_id, metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (workspace_id) DO UPDATE SET
    provider = EXCLUDED.provider,
    provider_api_key = EXCLUDED.provider_api_key,
    ai_gateway_api_key = EXCLUDED.ai_gateway_api_key,
    git_pat = EXCLUDED.git_pat,
    template_id = EXCLUDED.template_id,
    metadata = EXCLUDED.metadata,
    updated_at = now()
RETURNING *;

-- name: GetSandboxConfig :one
SELECT * FROM workspace_sandbox_config WHERE workspace_id = $1;

-- name: DeleteSandboxConfig :exec
DELETE FROM workspace_sandbox_config WHERE workspace_id = $1;

-- name: ListSandboxConfigs :many
SELECT workspace_id, provider, template_id, created_at, updated_at
FROM workspace_sandbox_config;
