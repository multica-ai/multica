-- FIR-3421: Operating System configuration foundation.
-- Per-workspace element toggles, definable goal types, and period units.

CREATE TABLE IF NOT EXISTS cerebro_os_element_setting (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    element_key TEXT NOT NULL CHECK (btrim(element_key) <> ''),
    enabled BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, element_key)
);

CREATE TABLE IF NOT EXISTS cerebro_goal_type (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL DEFAULT '#6366F1',
    scope_label TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_goal_type_name_not_blank CHECK (btrim(name) <> ''),
    UNIQUE (workspace_id, name)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_goal_type_workspace_position
    ON cerebro_goal_type (workspace_id, position, created_at);

ALTER TABLE cerebro_rock
    ADD COLUMN IF NOT EXISTS goal_type_id UUID REFERENCES cerebro_goal_type(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_cerebro_rock_goal_type
    ON cerebro_rock (workspace_id, goal_type_id) WHERE goal_type_id IS NOT NULL;

ALTER TABLE cerebro_operating_period
    ADD COLUMN IF NOT EXISTS unit TEXT NOT NULL DEFAULT 'quarter'
        CHECK (unit IN ('month', 'quarter', 'custom'));

-- Workspaces already using the Operating System keep their current labels.
-- Code-level defaults become neutral for workspaces configured after this point.
INSERT INTO cerebro_operating_system_settings (workspace_id, terminology)
SELECT DISTINCT workspace_id, '{"strategy":"Strategy","rock":"Rock","rocks":"Rocks"}'::jsonb
FROM (
    SELECT workspace_id FROM cerebro_rock
    UNION
    SELECT workspace_id FROM cerebro_strategy_item
) AS existing
ON CONFLICT (workspace_id) DO NOTHING;
