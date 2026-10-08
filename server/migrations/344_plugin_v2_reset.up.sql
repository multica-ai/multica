-- Plugin system reset.
--
-- The previous design shipped 14 tables, an append-only revision log per
-- relationship, a capability-snapshot compiler, and a per-task execution
-- manifest pinned by an INSERT trigger on agent_task_queue. It never left its
-- feature flag and holds no production data, so this migration removes it
-- outright rather than migrating it forward.
--
-- What replaces it: one installation row per (workspace, plugin), holding the
-- manifest snapshot the administrator actually consented to, plus two leaf
-- tables for plugin-owned state. Relationships stay application-owned by
-- repository policy: no foreign keys, no cascades.

DROP TRIGGER IF EXISTS trg_agent_task_queue_plugin_execution_manifest ON agent_task_queue;
DROP FUNCTION IF EXISTS pin_plugin_execution_manifest();

DROP TRIGGER IF EXISTS trg_plugin_installation_config_append_only ON plugin_installation_config;
DROP TRIGGER IF EXISTS trg_plugin_health_append_only ON plugin_health;
DROP TRIGGER IF EXISTS trg_plugin_execution_manifest_append_only ON plugin_execution_manifest;
DROP TRIGGER IF EXISTS trg_plugin_capability_snapshot_append_only ON plugin_capability_snapshot;
DROP TRIGGER IF EXISTS trg_plugin_artifact_file_append_only ON plugin_artifact_file;
DROP TRIGGER IF EXISTS trg_plugin_binding_append_only ON plugin_binding;
DROP TRIGGER IF EXISTS trg_plugin_grant_append_only ON plugin_grant;
DROP TRIGGER IF EXISTS trg_plugin_contribution_append_only ON plugin_contribution;
DROP TRIGGER IF EXISTS trg_plugin_release_immutable ON plugin_release;
DROP FUNCTION IF EXISTS reject_plugin_append_only_update();
DROP FUNCTION IF EXISTS enforce_plugin_release_immutable();

ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS plugin_execution_manifest_id;

DROP TABLE IF EXISTS plugin_remote_mcp_oauth_state;
DROP TABLE IF EXISTS plugin_remote_mcp_secret;
DROP TABLE IF EXISTS plugin_installation_config;
DROP TABLE IF EXISTS plugin_health;
DROP TABLE IF EXISTS plugin_execution_manifest;
DROP TABLE IF EXISTS plugin_capability_snapshot;
DROP TABLE IF EXISTS plugin_workspace_capability_state;
DROP TABLE IF EXISTS plugin_artifact_file;
DROP TABLE IF EXISTS plugin_binding;
DROP TABLE IF EXISTS plugin_grant;
DROP TABLE IF EXISTS plugin_installation;
DROP TABLE IF EXISTS plugin_contribution;
DROP TABLE IF EXISTS plugin_release;
DROP TABLE IF EXISTS plugin_identity;

-- One row per installed plugin. `manifest` is the snapshot the administrator
-- saw and approved on the consent screen, not whatever the source URL serves
-- today: an upgrade re-fetches, re-consents, and overwrites it. That is why
-- there is no separate version table.
CREATE TABLE plugin_installation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    plugin_key TEXT NOT NULL CHECK (char_length(plugin_key) BETWEEN 3 AND 255),
    source_url TEXT NOT NULL CHECK (char_length(source_url) BETWEEN 1 AND 2048),
    version TEXT NOT NULL CHECK (char_length(version) BETWEEN 1 AND 64),
    manifest JSONB NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    granted_scopes JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(granted_scopes) = 'array'),
    config JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    installed_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Plugin-owned key/value state. Exactly two scopes: workspace state shared by
-- the team, and per-member state. Quotas are enforced on write in application
-- code; nothing here is evicted behind the plugin's back.
CREATE TABLE plugin_storage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'user')),
    scope_id UUID NOT NULL,
    -- Byte-counted, matching the service-side quota: key and value are
    -- plugin-supplied and routinely non-ASCII.
    key TEXT NOT NULL CHECK (octet_length(key) BETWEEN 1 AND 1024),
    value TEXT NOT NULL CHECK (octet_length(value) <= 102400),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Secrets live in their own table so no storage read path can reach them by
-- construction. Values are encrypted with MULTICA_PLUGIN_SECRET_KEY and are
-- never returned by any endpoint.
CREATE TABLE plugin_secret (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    key TEXT NOT NULL CHECK (char_length(key) BETWEEN 1 AND 128),
    ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
