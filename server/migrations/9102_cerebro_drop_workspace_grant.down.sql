-- Reverse of 9102_cerebro_drop_workspace_grant.up.sql — recreate the
-- cerebro_workspace_grant control plane in its final pre-drop shape, mirroring
-- 9027 (base tables + indexes) and 9052 (the 'project' subject_type and the
-- capability lookup index). The tables come back empty; no grant data is
-- restored (none existed at drop time).

CREATE TABLE IF NOT EXISTS cerebro_workspace_grant (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- Subject
    subject_type TEXT NOT NULL,
    subject_id   UUID,
    CONSTRAINT cerebro_workspace_grant_subject_type_check CHECK (
        subject_type IN ('member', 'agent', 'group', 'role', 'workspace_default', 'project')
    ),

    -- Resource
    resource_pattern TEXT NOT NULL,

    -- Permission
    capability            TEXT NOT NULL,
    classification_ceiling TEXT,
    time_window_start     TIMESTAMPTZ,
    time_window_end       TIMESTAMPTZ,
    approval_required     BOOLEAN NOT NULL DEFAULT false,

    -- Lifecycle
    status     TEXT NOT NULL DEFAULT 'active',
    CONSTRAINT cerebro_workspace_grant_status_check CHECK (
        status IN ('active', 'revoked')
    ),

    -- Audit columns
    granted_by_type TEXT,
    granted_by_id   UUID,
    granted_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_by_id   UUID,
    revoked_at      TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_grant_workspace
    ON cerebro_workspace_grant (workspace_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_grant_subject
    ON cerebro_workspace_grant (workspace_id, subject_type, subject_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_grant_status
    ON cerebro_workspace_grant (workspace_id, status);

-- Capability lookup index (from 9052).
CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_grant_capability
    ON cerebro_workspace_grant (workspace_id, capability, status)
    WHERE status = 'active';

-- Audit ledger — append-only; grant_id is nullable so audit rows survive
-- hard-deletes (grant_id goes NULL on cascade).
CREATE TABLE IF NOT EXISTS cerebro_workspace_grant_audit (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    grant_id     UUID REFERENCES cerebro_workspace_grant(id) ON DELETE SET NULL,
    action       TEXT NOT NULL,
    CONSTRAINT cerebro_workspace_grant_audit_action_check CHECK (
        action IN ('created', 'updated', 'revoked', 'deleted')
    ),
    actor_type   TEXT,
    actor_id     UUID,
    surface      TEXT NOT NULL DEFAULT 'api',
    CONSTRAINT cerebro_workspace_grant_audit_surface_check CHECK (
        surface IN ('api', 'cli', 'ui', 'mcp')
    ),
    diff         JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_grant_audit_grant
    ON cerebro_workspace_grant_audit (grant_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_workspace_grant_audit_workspace
    ON cerebro_workspace_grant_audit (workspace_id, created_at DESC);
