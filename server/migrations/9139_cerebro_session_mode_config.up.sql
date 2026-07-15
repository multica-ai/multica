CREATE TABLE IF NOT EXISTS cerebro_session_mode_registry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('plan', 'build', 'research', 'review')),
    active_version INTEGER NOT NULL DEFAULT 1 CHECK (active_version > 0),
    draft JSONB,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, mode)
);

CREATE TABLE IF NOT EXISTS cerebro_session_mode_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    registry_id UUID NOT NULL REFERENCES cerebro_session_mode_registry(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    config JSONB NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (registry_id, version)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_session_mode_registry_workspace
    ON cerebro_session_mode_registry(workspace_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_session_mode_version_registry_created
    ON cerebro_session_mode_version(registry_id, version DESC);

