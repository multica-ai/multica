-- Phase 6: per-environment adapter configuration. The config + webhook
-- secret are stored encrypted; decryption happens at read-time in the
-- service layer via pkg/secrets.DecryptString.

-- name: GetDeployAdapterConfig :one
SELECT * FROM deploy_adapter_config WHERE environment_id = $1;

-- name: ListDeployAdapterConfigsByKind :many
-- Webhook ingestion path: the receiver knows the adapter_kind from the URL
-- (/api/integrations/deploy/{adapter}/webhook) but needs to scan every
-- candidate env to find the one whose decrypted config matches the
-- payload's identifier (vercel project_id, cloudflare project_name, etc.).
-- Bounded by adapter_kind so we don't scan the whole table.
SELECT * FROM deploy_adapter_config WHERE adapter_kind = $1;

-- name: UpsertDeployAdapterConfig :one
-- Called from PUT /api/deploy_environments/{id}/adapter. Atomic upsert
-- so the same call works for both initial setup and reconfiguration. The
-- adapter_kind is also written back onto deploy_environment in a separate
-- statement so the FK row carries the canonical value for routing.
INSERT INTO deploy_adapter_config (
    environment_id, adapter_kind, config_encrypted, webhook_secret_encrypted
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (environment_id) DO UPDATE SET
    adapter_kind             = EXCLUDED.adapter_kind,
    config_encrypted         = EXCLUDED.config_encrypted,
    -- Allow rotating just the secret without re-uploading the whole config:
    -- the handler passes NULL for webhook_secret_encrypted to mean "leave
    -- alone". COALESCE on the EXCLUDED row preserves prior value when the
    -- handler omits it.
    webhook_secret_encrypted = COALESCE(EXCLUDED.webhook_secret_encrypted, deploy_adapter_config.webhook_secret_encrypted),
    updated_at               = now()
RETURNING *;

-- name: DeleteDeployAdapterConfig :exec
-- Used when reverting an env back to github_actions (the default) — the
-- row no longer carries useful state.
DELETE FROM deploy_adapter_config WHERE environment_id = $1;

-- name: SetDeployEnvironmentAdapterKind :exec
-- Mirror UpsertDeployAdapterConfig.adapter_kind onto the parent row so the
-- webhook router can find the env without joining the encrypted table.
UPDATE deploy_environment SET adapter_kind = $2, updated_at = now()
WHERE id = $1;

-- name: ListDeployEnvironmentsByAdapter :many
-- Used by the periodic poller — iterate every env whose adapter supports
-- pull-based polling. The adapter registry decides which kinds qualify.
SELECT * FROM deploy_environment
WHERE adapter_kind = ANY(sqlc.arg('kinds')::text[])
ORDER BY id;
