-- Ship Hub Phase 6: deploy adapters beyond GitHub Actions.
--
-- Phase 1-5 hard-coded GitHub-Actions-as-the-deploy-mechanism into the
-- ingestion path: a single webhook receiver, no per-environment adapter
-- config, no rollback dispatch. Real-world Multica workspaces deploy via
-- many platforms (Vercel, Cloudflare Pages, Fly, Render, custom CI).
-- Phase 6 makes the adapter axis multi-tenant.
--
-- Schema additions:
--
-- - deploy_environment.adapter_kind: which adapter handles inbound webhook
--   events / outbound poll + rollback for this env. Defaults to
--   'github_actions' for migration safety so the prior behavior survives
--   without explicit reconfiguration.
--
-- - deploy_adapter_config: per-env encrypted config + webhook secret. Two
--   columns rather than one because the provider's API token (config.token)
--   and the inbound webhook signing secret are independent — rotating the
--   token must not invalidate webhooks (and vice versa). AES-256-GCM via
--   pkg/secrets, same key as workspace_secret.
--
-- The reverse drop is in 084_ship_hub_phase_6.down.sql.

ALTER TABLE deploy_environment
    ADD COLUMN adapter_kind TEXT NOT NULL DEFAULT 'github_actions';

-- Per-environment adapter configuration. PRIMARY KEY (environment_id) so
-- the table is effectively a 1:1 extension of deploy_environment — the
-- adapter row is created lazily the first time the env is configured for a
-- non-default adapter.
CREATE TABLE deploy_adapter_config (
    environment_id           UUID PRIMARY KEY REFERENCES deploy_environment(id) ON DELETE CASCADE,
    adapter_kind             TEXT NOT NULL,
    -- AES-256-GCM with prepended 12-byte nonce. Decrypts via
    -- pkg/secrets.DecryptString. Stores adapter-specific JSON
    -- (e.g. Vercel: {team_id, project_id, token}; Cloudflare: {account_id,
    -- project_name, api_token}). Always encrypted: the provider tokens
    -- are write-credentials and must never live in cleartext.
    config_encrypted         BYTEA NOT NULL,
    -- Optional inbound-webhook signing secret. Separate from config
    -- because providers sign their own webhook deliveries with a secret
    -- the workspace owner pastes into BOTH Multica and the provider's UI.
    -- NULL when the adapter doesn't support webhooks (e.g. fly.io).
    webhook_secret_encrypted BYTEA,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Webhook ingestion needs to look up the env by adapter+identifier (e.g.
-- vercel project_id, cloudflare project_name). The identifier is hashed
-- inside the encrypted config blob, but the adapter_kind alone is enough
-- to bound the candidate set to a small per-workspace count — the linear
-- scan inside the receiver is fine at the scale Ship Hub targets.
CREATE INDEX idx_deploy_adapter_config_kind
    ON deploy_adapter_config(adapter_kind);
