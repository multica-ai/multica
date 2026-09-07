-- Plugin V1 lifecycle foundation.
--
-- This migration deliberately stores only the generic Plugin spine. The first
-- contribution adapter (agent.skill.v1), capability snapshots, and execution
-- manifests build on these objects in later migrations.
--
-- Relationships are application-owned by repository policy: there are no
-- foreign keys or cascades. Write queries resolve and lock parent rows, and the
-- workspace teardown deletes installation descendants explicitly.

CREATE TABLE plugin_identity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plugin_key TEXT NOT NULL CHECK (char_length(plugin_key) BETWEEN 3 AND 255),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 160),
    publisher_id TEXT NOT NULL CHECK (char_length(publisher_id) BETWEEN 1 AND 255),
    publisher_type TEXT NOT NULL CHECK (
        publisher_type IN ('official', 'verified', 'community', 'private_dev')
    ),
    trust_tier TEXT NOT NULL CHECK (
        trust_tier IN ('official', 'verified', 'community', 'private_dev')
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    retired_at TIMESTAMPTZ
);

CREATE TABLE plugin_release (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plugin_id UUID NOT NULL,
    version TEXT NOT NULL CHECK (char_length(version) BETWEEN 1 AND 128),
    manifest JSONB NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    manifest_digest TEXT NOT NULL CHECK (manifest_digest ~ '^sha256:[0-9a-f]{64}$'),
    source_kind TEXT NOT NULL CHECK (source_kind IN ('bundled', 'private_dev')),
    source_ref TEXT NOT NULL CHECK (char_length(source_ref) BETWEEN 1 AND 2048),
    archive_digest TEXT NOT NULL CHECK (archive_digest ~ '^sha256:[0-9a-f]{64}$'),
    artifact_ref TEXT NOT NULL CHECK (char_length(artifact_ref) BETWEEN 1 AND 2048),
    artifact_digest TEXT NOT NULL CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
    artifact_size BIGINT NOT NULL CHECK (artifact_size BETWEEN 0 AND 33554432),
    signature BYTEA,
    signature_key_id TEXT,
    revocation_status TEXT NOT NULL DEFAULT 'active' CHECK (
        revocation_status IN ('active', 'revoked', 'quarantined')
    ),
    revoked_at TIMESTAMPTZ,
    revocation_reason TEXT,
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((signature IS NULL) = (signature_key_id IS NULL)),
    CHECK (source_kind <> 'bundled' OR signature IS NOT NULL),
    CHECK (
        (revocation_status = 'active' AND revoked_at IS NULL AND revocation_reason IS NULL)
        OR
        (revocation_status <> 'active' AND revoked_at IS NOT NULL AND char_length(revocation_reason) > 0)
    )
);

CREATE TABLE plugin_contribution (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id UUID NOT NULL,
    contribution_key TEXT NOT NULL CHECK (char_length(contribution_key) BETWEEN 1 AND 128),
    type TEXT NOT NULL CHECK (type IN ('agent.skill.v1')),
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    display_name TEXT NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 160),
    description TEXT NOT NULL,
    entry_path TEXT NOT NULL CHECK (char_length(entry_path) BETWEEN 1 AND 1024),
    entry_digest TEXT NOT NULL CHECK (entry_digest ~ '^sha256:[0-9a-f]{64}$'),
    artifact_digest TEXT NOT NULL CHECK (artifact_digest ~ '^sha256:[0-9a-f]{64}$'),
    required_daemon_features JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (
        jsonb_typeof(required_daemon_features) = 'array'
    ),
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plugin_installation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    plugin_id UUID NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('bundled', 'private_dev')),
    source_ref TEXT NOT NULL CHECK (char_length(source_ref) BETWEEN 1 AND 2048),
    desired_release_id UUID NOT NULL,
    active_release_id UUID,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    desired_generation BIGINT NOT NULL DEFAULT 1 CHECK (desired_generation > 0),
    active_generation BIGINT NOT NULL DEFAULT 0 CHECK (active_generation >= 0),
    lifecycle_status TEXT NOT NULL DEFAULT 'installed' CHECK (
        lifecycle_status IN (
            'installed', 'activating', 'active', 'degraded', 'error',
            'uninstalling', 'uninstalled'
        )
    ),
    installed_by UUID,
    installed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ,
    uninstalled_at TIMESTAMPTZ,
    CHECK (active_generation <= desired_generation),
    CHECK (active_generation > 0 OR active_release_id IS NULL),
    CHECK (
        (lifecycle_status = 'uninstalled' AND uninstalled_at IS NOT NULL AND enabled = FALSE)
        OR
        (lifecycle_status <> 'uninstalled' AND uninstalled_at IS NULL)
    )
);

-- Grants and bindings are append-only revision logs. A new decision inserts a
-- new revision; readers select the greatest revision per logical key.
CREATE TABLE plugin_grant (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    capability TEXT NOT NULL CHECK (char_length(capability) BETWEEN 1 AND 255),
    decision TEXT NOT NULL CHECK (decision IN ('granted', 'denied')),
    limits JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(limits) = 'object'),
    grant_revision BIGINT NOT NULL CHECK (grant_revision > 0),
    approved_by UUID,
    approved_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ
);

CREATE TABLE plugin_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'agent')),
    scope_id UUID NOT NULL,
    enabled BOOLEAN NOT NULL,
    binding_revision BIGINT NOT NULL CHECK (binding_revision > 0),
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Release identity/content and contribution rows are immutable. Revocation is
-- the only release mutation and is a one-way transition out of active.
CREATE FUNCTION enforce_plugin_release_immutable()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.plugin_id IS DISTINCT FROM OLD.plugin_id
       OR NEW.version IS DISTINCT FROM OLD.version
       OR NEW.manifest IS DISTINCT FROM OLD.manifest
       OR NEW.manifest_digest IS DISTINCT FROM OLD.manifest_digest
       OR NEW.source_kind IS DISTINCT FROM OLD.source_kind
       OR NEW.source_ref IS DISTINCT FROM OLD.source_ref
       OR NEW.archive_digest IS DISTINCT FROM OLD.archive_digest
       OR NEW.artifact_ref IS DISTINCT FROM OLD.artifact_ref
       OR NEW.artifact_digest IS DISTINCT FROM OLD.artifact_digest
       OR NEW.artifact_size IS DISTINCT FROM OLD.artifact_size
       OR NEW.signature IS DISTINCT FROM OLD.signature
       OR NEW.signature_key_id IS DISTINCT FROM OLD.signature_key_id
       OR NEW.published_at IS DISTINCT FROM OLD.published_at THEN
        RAISE EXCEPTION 'plugin releases are immutable';
    END IF;

    IF OLD.revocation_status <> 'active' THEN
        RAISE EXCEPTION 'plugin release revocation is immutable';
    END IF;

    IF NEW.revocation_status = 'active' THEN
        RAISE EXCEPTION 'plugin release update must revoke or quarantine';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_plugin_release_immutable
BEFORE UPDATE ON plugin_release
FOR EACH ROW EXECUTE FUNCTION enforce_plugin_release_immutable();

CREATE FUNCTION reject_plugin_append_only_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% rows are append-only', TG_TABLE_NAME;
END;
$$;

CREATE TRIGGER trg_plugin_contribution_append_only
BEFORE UPDATE ON plugin_contribution
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();

CREATE TRIGGER trg_plugin_grant_append_only
BEFORE UPDATE ON plugin_grant
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();

CREATE TRIGGER trg_plugin_binding_append_only
BEFORE UPDATE ON plugin_binding
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();
