-- FIR-3421: editable Vision Plan sections and inline content.

CREATE TABLE IF NOT EXISTS cerebro_vision_plan_section (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    section_type TEXT NOT NULL DEFAULT 'list'
        CHECK (section_type IN ('list', 'structured', 'process')),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_vision_plan_section_key_not_blank CHECK (btrim(key) <> ''),
    CONSTRAINT cerebro_vision_plan_section_name_not_blank CHECK (btrim(name) <> ''),
    UNIQUE (workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_vision_plan_section_workspace_position
    ON cerebro_vision_plan_section (workspace_id, position, created_at);

ALTER TABLE cerebro_strategy_item
    ADD COLUMN IF NOT EXISTS section_id UUID REFERENCES cerebro_vision_plan_section(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS part_label TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS owner_type TEXT,
    ADD COLUMN IF NOT EXISTS owner_id UUID;

ALTER TABLE cerebro_strategy_item
    DROP CONSTRAINT IF EXISTS cerebro_strategy_item_owner_type_check,
    DROP CONSTRAINT IF EXISTS cerebro_strategy_item_owner_pair_check,
    ADD CONSTRAINT cerebro_strategy_item_owner_type_check
        CHECK (owner_type IS NULL OR owner_type IN ('member', 'agent')),
    ADD CONSTRAINT cerebro_strategy_item_owner_pair_check
        CHECK ((owner_type IS NULL) = (owner_id IS NULL));

CREATE INDEX IF NOT EXISTS idx_cerebro_strategy_item_section_position
    ON cerebro_strategy_item (workspace_id, section_id, position, created_at);

WITH workspaces AS (
    SELECT DISTINCT workspace_id FROM cerebro_strategy_item
), defaults(key, name, section_type, position) AS (
    VALUES
        ('core-values', 'Core Values', 'list', 0),
        ('core-focus', 'Core Focus', 'list', 1),
        ('long-term-target', 'Long-Term Target', 'list', 2),
        ('marketing-strategy', 'Marketing Strategy', 'structured', 3),
        ('three-year-picture', 'Three-Year Picture', 'list', 4),
        ('one-year-plan', 'One-Year Plan', 'list', 5),
        ('quarterly-goals', 'Quarterly Goals', 'list', 6),
        ('issues-list', 'Issues List', 'list', 7),
        ('core-processes', 'Core Processes', 'process', 8)
)
INSERT INTO cerebro_vision_plan_section (workspace_id, key, name, section_type, position)
SELECT w.workspace_id, d.key, d.name, d.section_type, d.position
FROM workspaces w CROSS JOIN defaults d
ON CONFLICT (workspace_id, key) DO NOTHING;

INSERT INTO cerebro_vision_plan_section (workspace_id, key, name, section_type, position)
SELECT DISTINCT item.workspace_id,
       'legacy-horizon-' || md5(COALESCE(NULLIF(item.horizon_label, ''), item.horizon_count::text || '-' || item.horizon_unit)),
       COALESCE(NULLIF(item.horizon_label, ''), item.horizon_count::text || '-' || initcap(item.horizon_unit) || ' Horizon'),
       'list', 6
FROM cerebro_strategy_item item
WHERE item.kind = 'horizon_goal'
  AND NOT (item.horizon_unit = 'year' AND item.horizon_count IN (1, 3))
  AND NOT (item.horizon_unit = 'year' AND item.horizon_count >= 4)
ON CONFLICT (workspace_id, key) DO NOTHING;

UPDATE cerebro_strategy_item item
SET section_id = section.id
FROM cerebro_vision_plan_section section
WHERE section.workspace_id = item.workspace_id
  AND item.section_id IS NULL
  AND section.key = CASE
      WHEN item.kind = 'core_value' THEN 'core-values'
      WHEN item.kind = 'core_focus' THEN 'core-focus'
      WHEN item.horizon_unit = 'year' AND item.horizon_count >= 4 THEN 'long-term-target'
      WHEN item.horizon_unit = 'year' AND item.horizon_count = 3 THEN 'three-year-picture'
      WHEN item.horizon_unit = 'year' AND item.horizon_count = 1 THEN 'one-year-plan'
      ELSE 'legacy-horizon-' || md5(COALESCE(NULLIF(item.horizon_label, ''), item.horizon_count::text || '-' || item.horizon_unit))
  END;
