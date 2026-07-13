-- FIR-2816: standalone Strategy and Rocks foundation.
-- Rocks extend existing projects; issue rows remain the execution truth.

CREATE TABLE IF NOT EXISTS cerebro_operating_system_settings (
    workspace_id UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    terminology JSONB NOT NULL DEFAULT '{"strategy":"Strategy","rock":"Rock","rocks":"Rocks"}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(terminology) = 'object')
);

CREATE TABLE IF NOT EXISTS cerebro_strategy_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('core_value', 'core_focus', 'horizon_goal')),
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    horizon_unit TEXT CHECK (horizon_unit IN ('day', 'week', 'month', 'year')),
    horizon_count INTEGER CHECK (horizon_count > 0),
    position INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (kind = 'horizon_goal' AND horizon_unit IS NOT NULL AND horizon_count IS NOT NULL)
        OR (kind <> 'horizon_goal' AND horizon_unit IS NULL AND horizon_count IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_cerebro_strategy_item_workspace_position
    ON cerebro_strategy_item (workspace_id, position, created_at);

CREATE TABLE IF NOT EXISTS cerebro_rock (
    project_id UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    confidence INTEGER NOT NULL DEFAULT 50 CHECK (confidence BETWEEN 0 AND 100),
    reported_health TEXT NOT NULL DEFAULT 'unset'
        CHECK (reported_health IN ('on_track', 'at_risk', 'off_track', 'unset')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_end >= period_start)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_rock_workspace_period
    ON cerebro_rock (workspace_id, period_start, period_end);

CREATE TABLE IF NOT EXISTS cerebro_object_connection (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL,
    source_id UUID NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    relationship_type TEXT NOT NULL DEFAULT 'relates_to',
    provenance TEXT NOT NULL DEFAULT 'manual' CHECK (provenance IN ('manual', 'agent', 'system')),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, source_type, source_id, target_type, target_id, relationship_type)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_object_connection_source
    ON cerebro_object_connection (workspace_id, source_type, source_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_object_connection_target
    ON cerebro_object_connection (workspace_id, target_type, target_id);
