-- Plugin V1 capability snapshots, task execution manifests, and immutable
-- artifact files. The workspace state row is the only mutable pointer: every
-- snapshot and execution manifest remains append-only for replay/audit.

CREATE TABLE plugin_artifact_file (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    release_id UUID NOT NULL,
    path TEXT NOT NULL CHECK (char_length(path) BETWEEN 1 AND 1024),
    digest TEXT NOT NULL CHECK (digest ~ '^sha256:[0-9a-f]{64}$'),
    size_bytes BIGINT NOT NULL CHECK (size_bytes BETWEEN 0 AND 1048576),
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(content) = size_bytes)
);

CREATE TABLE plugin_workspace_capability_state (
    workspace_id UUID PRIMARY KEY,
    next_revision BIGINT NOT NULL DEFAULT 1 CHECK (next_revision > 0),
    active_snapshot_id UUID,
    active_revision BIGINT NOT NULL DEFAULT 0 CHECK (active_revision >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((active_snapshot_id IS NULL) = (active_revision = 0))
);

CREATE TABLE plugin_capability_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    source_generations JSONB NOT NULL CHECK (jsonb_typeof(source_generations) = 'object'),
    compiler_version TEXT NOT NULL CHECK (char_length(compiler_version) BETWEEN 1 AND 128),
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    snapshot_digest TEXT NOT NULL CHECK (snapshot_digest ~ '^sha256:[0-9a-f]{64}$'),
    compiled_entries JSONB NOT NULL CHECK (jsonb_typeof(compiled_entries) = 'array'),
    diagnostics JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(diagnostics) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plugin_execution_manifest (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    snapshot_id UUID,
    snapshot_revision BIGINT NOT NULL DEFAULT 0 CHECK (snapshot_revision >= 0),
    snapshot_digest TEXT,
    composer_version TEXT NOT NULL CHECK (char_length(composer_version) BETWEEN 1 AND 128),
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    ordered_contributions JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (
        jsonb_typeof(ordered_contributions) = 'array'
    ),
    diagnostics JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(diagnostics) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((snapshot_id IS NULL) = (snapshot_revision = 0)),
    CHECK ((snapshot_id IS NULL) = (snapshot_digest IS NULL)),
    CHECK (snapshot_digest IS NULL OR snapshot_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE TABLE plugin_health (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'agent')),
    scope_id UUID,
    state TEXT NOT NULL CHECK (state IN ('healthy', 'degraded', 'error', 'disabled')),
    reason_code TEXT NOT NULL CHECK (char_length(reason_code) BETWEEN 1 AND 128),
    safe_detail TEXT NOT NULL DEFAULT '',
    observed_generation BIGINT NOT NULL CHECK (observed_generation > 0),
    last_good_snapshot_id UUID,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE agent_task_queue
    ADD COLUMN plugin_execution_manifest_id UUID;

CREATE TRIGGER trg_plugin_artifact_file_append_only
BEFORE UPDATE ON plugin_artifact_file
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();

CREATE TRIGGER trg_plugin_capability_snapshot_append_only
BEFORE UPDATE ON plugin_capability_snapshot
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();

CREATE TRIGGER trg_plugin_execution_manifest_append_only
BEFORE UPDATE ON plugin_execution_manifest
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();

CREATE TRIGGER trg_plugin_health_append_only
BEFORE UPDATE ON plugin_health
FOR EACH ROW EXECUTE FUNCTION reject_plugin_append_only_update();

CREATE FUNCTION pin_plugin_execution_manifest()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    workspace UUID;
    source_manifest plugin_execution_manifest%ROWTYPE;
    active_snapshot plugin_capability_snapshot%ROWTYPE;
    manifest_id UUID;
    contributions JSONB := '[]'::jsonb;
BEGIN
    SELECT agent.workspace_id INTO workspace
    FROM agent
    WHERE agent.id = NEW.agent_id;

    IF workspace IS NULL THEN
        RAISE EXCEPTION 'cannot compose plugin execution manifest without task workspace';
    END IF;

    IF NEW.retry_of_task_id IS NOT NULL THEN
        SELECT plugin_execution_manifest.* INTO source_manifest
        FROM agent_task_queue parent
        JOIN plugin_execution_manifest
          ON plugin_execution_manifest.id = parent.plugin_execution_manifest_id
        WHERE parent.id = NEW.retry_of_task_id;
    END IF;

    IF source_manifest.id IS NOT NULL THEN
        INSERT INTO plugin_execution_manifest (
            task_id, workspace_id, agent_id, runtime_id,
            snapshot_id, snapshot_revision, snapshot_digest,
            composer_version, schema_version,
            ordered_contributions, diagnostics
        ) VALUES (
            NEW.id, workspace, NEW.agent_id, NEW.runtime_id,
            source_manifest.snapshot_id,
            source_manifest.snapshot_revision,
            source_manifest.snapshot_digest,
            source_manifest.composer_version,
            source_manifest.schema_version,
            source_manifest.ordered_contributions,
            source_manifest.diagnostics
        ) RETURNING id INTO manifest_id;
    ELSE
        SELECT snapshot.* INTO active_snapshot
        FROM plugin_workspace_capability_state state
        JOIN plugin_capability_snapshot snapshot
          ON snapshot.id = state.active_snapshot_id
        WHERE state.workspace_id = workspace;

        IF active_snapshot.id IS NOT NULL THEN
            SELECT COALESCE(jsonb_agg(entry ORDER BY
                COALESCE((entry->>'ordinal')::integer, 0),
                entry->>'plugin_key',
                entry->>'contribution_key'
            ), '[]'::jsonb)
            INTO contributions
            FROM jsonb_array_elements(active_snapshot.compiled_entries) AS entry
            WHERE (
                entry->>'scope_type' = 'agent'
                AND entry->>'scope_id' = NEW.agent_id::text
            ) OR (
                entry->>'scope_type' = 'workspace'
                AND NOT COALESCE(entry->'excluded_agent_ids', '[]'::jsonb) ? NEW.agent_id::text
            );
        END IF;

        INSERT INTO plugin_execution_manifest (
            task_id, workspace_id, agent_id, runtime_id,
            snapshot_id, snapshot_revision, snapshot_digest,
            composer_version, schema_version,
            ordered_contributions, diagnostics
        ) VALUES (
            NEW.id, workspace, NEW.agent_id, NEW.runtime_id,
            active_snapshot.id,
            COALESCE(active_snapshot.revision, 0),
            active_snapshot.snapshot_digest,
            'plugin-composer-v1', 1,
            contributions, '[]'::jsonb
        ) RETURNING id INTO manifest_id;
    END IF;

    NEW.plugin_execution_manifest_id = manifest_id;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_agent_task_queue_plugin_execution_manifest
BEFORE INSERT ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION pin_plugin_execution_manifest();
