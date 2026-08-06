-- FIR-3589: Vision/Traction becomes building blocks the workspace arranges
-- itself. A page owns an ordered set of columns; every section is a block that
-- sits in one column of one page. Pages are user-creatable, so Vision and
-- Traction stop being hard-coded layouts and become seeded data.

CREATE TABLE IF NOT EXISTS cerebro_vision_plan_page (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    column_count INTEGER NOT NULL DEFAULT 3,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_vision_plan_page_key_not_blank CHECK (btrim(key) <> ''),
    CONSTRAINT cerebro_vision_plan_page_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT cerebro_vision_plan_page_column_count_range CHECK (column_count BETWEEN 1 AND 3),
    UNIQUE (workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_vision_plan_page_workspace_position
    ON cerebro_vision_plan_page (workspace_id, position, created_at);

ALTER TABLE cerebro_vision_plan_section
    ADD COLUMN IF NOT EXISTS page_id UUID REFERENCES cerebro_vision_plan_page(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS column_index INTEGER NOT NULL DEFAULT 0;

ALTER TABLE cerebro_vision_plan_section
    DROP CONSTRAINT IF EXISTS cerebro_vision_plan_section_section_type_check,
    ADD CONSTRAINT cerebro_vision_plan_section_section_type_check
        CHECK (section_type IN ('list', 'structured', 'process', 'goals'));

CREATE INDEX IF NOT EXISTS idx_cerebro_vision_plan_section_page_column_position
    ON cerebro_vision_plan_section (workspace_id, page_id, column_index, position, created_at);
