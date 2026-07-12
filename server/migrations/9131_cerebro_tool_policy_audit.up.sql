-- FIR-3091 punkt 8 fase 2 — change log for tool-policy rows.
--
-- Every Set/Clear on cerebro_tool_policy leaves one row here so the
-- per-permission detail page can show "all changes" for a tool. The table is
-- append-only history: cerebro_tool_policy itself keeps only current state
-- (updated_by/updated_at of the LAST write), so without this log a change is
-- unrecoverable the moment it is overwritten or cleared. History starts the
-- day this ships — there is no backfill source.
--
-- Shape mirrors cerebro_credential_audit (the one existing audit trail) so the
-- two logs read the same: actor_type/actor_id + created_at, with the
-- policy-specific dimensions (layer/subject/resource_pattern) and the
-- old_setting -> new_setting transition added.

CREATE TABLE IF NOT EXISTS cerebro_tool_policy_audit (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tool_key          TEXT NOT NULL,
    layer             TEXT NOT NULL,
    subject_id        UUID NOT NULL,
    resource_pattern  TEXT NOT NULL DEFAULT '',
    action            TEXT NOT NULL CHECK (action IN ('set', 'clear')),
    -- old_setting '' means "no explicit row before this write" (the layer was
    -- Inherit); new_setting '' means the row was cleared back to Inherit.
    old_setting       TEXT NOT NULL DEFAULT '',
    new_setting       TEXT NOT NULL DEFAULT '',
    actor_type        TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id          UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The detail page reads one tool's history newest-first.
CREATE INDEX IF NOT EXISTS idx_cerebro_tool_policy_audit_tool
    ON cerebro_tool_policy_audit (workspace_id, tool_key, created_at DESC);
