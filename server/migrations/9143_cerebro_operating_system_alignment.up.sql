-- FIR-2816: align Operating System with the approved v4 model.
-- Existing project-backed Rocks become first-class Rocks and retain a Project link.

CREATE TABLE IF NOT EXISTS cerebro_operating_period (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    starts_on DATE NOT NULL,
    ends_on DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (ends_on >= starts_on),
    UNIQUE (workspace_id, starts_on, ends_on)
);

INSERT INTO cerebro_operating_period (workspace_id, name, starts_on, ends_on)
SELECT DISTINCT
    workspace_id,
    'Q' || EXTRACT(QUARTER FROM period_start)::integer || ' ' || EXTRACT(YEAR FROM period_start)::integer,
    period_start,
    period_end
FROM cerebro_rock
ON CONFLICT (workspace_id, starts_on, ends_on) DO NOTHING;

ALTER TABLE cerebro_strategy_item
    ADD COLUMN IF NOT EXISTS horizon_label TEXT;

UPDATE cerebro_strategy_item
SET horizon_label = CASE
    WHEN horizon_count = 10 AND horizon_unit = 'year' THEN '10-Year Target'
    WHEN horizon_count = 3 AND horizon_unit = 'year' THEN '3-Year Picture'
    WHEN horizon_count = 1 AND horizon_unit = 'year' THEN '1-Year Plan'
    ELSE horizon_count || '-' ||
        CASE horizon_unit
            WHEN 'day' THEN 'Day'
            WHEN 'week' THEN 'Week'
            WHEN 'month' THEN 'Month'
            WHEN 'year' THEN 'Year'
        END || ' Horizon'
    END
WHERE kind = 'horizon_goal' AND horizon_label IS NULL;

ALTER TABLE cerebro_rock
    ADD COLUMN IF NOT EXISTS id UUID DEFAULT gen_random_uuid(),
    ADD COLUMN IF NOT EXISTS title TEXT,
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS owner_type TEXT,
    ADD COLUMN IF NOT EXISTS owner_id UUID,
    ADD COLUMN IF NOT EXISTS period_id UUID REFERENCES cerebro_operating_period(id) ON DELETE RESTRICT;

UPDATE cerebro_rock r
SET title = p.title,
    description = COALESCE(p.description, ''),
    owner_type = p.lead_type,
    owner_id = p.lead_id,
    period_id = op.id
FROM project p, cerebro_operating_period op
WHERE p.id = r.project_id
  AND p.workspace_id = r.workspace_id
  AND op.workspace_id = r.workspace_id
  AND op.starts_on = r.period_start
  AND op.ends_on = r.period_end
  AND (r.title IS NULL OR r.period_id IS NULL);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM pg_constraint c
        JOIN pg_attribute a
            ON a.attrelid = c.conrelid
           AND a.attnum = ANY(c.conkey)
        WHERE c.conrelid = 'cerebro_rock'::regclass
          AND c.conname = 'cerebro_rock_pkey'
          AND a.attname = 'project_id'
    ) THEN
        ALTER TABLE cerebro_rock DROP CONSTRAINT cerebro_rock_pkey;
    END IF;
END $$;

ALTER TABLE cerebro_rock
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN title SET NOT NULL,
    ALTER COLUMN period_id SET NOT NULL,
    ALTER COLUMN project_id DROP NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'cerebro_rock'::regclass
          AND conname = 'cerebro_rock_pkey'
    ) THEN
        ALTER TABLE cerebro_rock ADD CONSTRAINT cerebro_rock_pkey PRIMARY KEY (id);
    END IF;
END $$;

ALTER TABLE cerebro_rock
    DROP CONSTRAINT IF EXISTS cerebro_rock_owner_type_check,
    DROP CONSTRAINT IF EXISTS cerebro_rock_title_not_blank,
    ADD CONSTRAINT cerebro_rock_owner_type_check
        CHECK (owner_type IS NULL OR owner_type IN ('member', 'agent')),
    ADD CONSTRAINT cerebro_rock_title_not_blank CHECK (btrim(title) <> '');
CREATE INDEX IF NOT EXISTS idx_cerebro_rock_workspace_period_v2
    ON cerebro_rock (workspace_id, period_id, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_rock_legacy_project
    ON cerebro_rock (project_id) WHERE project_id IS NOT NULL;

ALTER TABLE cerebro_object_connection
    DROP CONSTRAINT IF EXISTS cerebro_object_connection_created_by_type_check,
    ADD CONSTRAINT cerebro_object_connection_created_by_type_check
        CHECK (created_by_type IN ('member', 'agent', 'system'));

-- Existing Strategy links pointed at the legacy Project id. Preserve them by
-- retargeting to the new first-class Rock id.
UPDATE cerebro_object_connection c
SET target_id = r.id
FROM cerebro_rock r
WHERE c.workspace_id = r.workspace_id
  AND c.target_type = 'rock'
  AND c.target_id = r.project_id
  AND r.project_id IS NOT NULL;

INSERT INTO cerebro_object_connection (
    workspace_id, source_type, source_id, target_type, target_id,
    relationship_type, provenance, created_by_type, created_by_id
)
SELECT
    workspace_id, 'rock', id, 'project', project_id,
    'contains', 'system', 'system', id
FROM cerebro_rock
WHERE project_id IS NOT NULL
ON CONFLICT (workspace_id, source_type, source_id, target_type, target_id, relationship_type)
DO NOTHING;

CREATE TABLE IF NOT EXISTS cerebro_rock_check_in (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    rock_id UUID NOT NULL REFERENCES cerebro_rock(id) ON DELETE CASCADE,
    confidence INTEGER NOT NULL CHECK (confidence BETWEEN 0 AND 100),
    reported_health TEXT NOT NULL
        CHECK (reported_health IN ('on_track', 'at_risk', 'off_track', 'unset')),
    note TEXT NOT NULL DEFAULT '',
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_rock_check_in_rock_created
    ON cerebro_rock_check_in (workspace_id, rock_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_strategy_item_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    strategy_item_id UUID NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('baseline', 'created', 'updated', 'deleted')),
    title TEXT NOT NULL,
    snapshot JSONB NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_strategy_history_workspace_changed
    ON cerebro_strategy_item_history (workspace_id, changed_at DESC);

INSERT INTO cerebro_strategy_item_history (
    workspace_id, strategy_item_id, action, title, snapshot, changed_at
)
SELECT workspace_id, id, 'baseline', title, to_jsonb(cerebro_strategy_item), updated_at
FROM cerebro_strategy_item
WHERE NOT EXISTS (
    SELECT 1
    FROM cerebro_strategy_item_history h
    WHERE h.workspace_id = cerebro_strategy_item.workspace_id
      AND h.strategy_item_id = cerebro_strategy_item.id
      AND h.action = 'baseline'
);

CREATE OR REPLACE FUNCTION cerebro_record_strategy_item_history() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO cerebro_strategy_item_history (workspace_id, strategy_item_id, action, title, snapshot)
        VALUES (OLD.workspace_id, OLD.id, 'deleted', OLD.title, to_jsonb(OLD));
        RETURN OLD;
    ELSIF TG_OP = 'INSERT' THEN
        INSERT INTO cerebro_strategy_item_history (workspace_id, strategy_item_id, action, title, snapshot)
        VALUES (NEW.workspace_id, NEW.id, 'created', NEW.title, to_jsonb(NEW));
        RETURN NEW;
    END IF;
    INSERT INTO cerebro_strategy_item_history (workspace_id, strategy_item_id, action, title, snapshot)
    VALUES (NEW.workspace_id, NEW.id, 'updated', NEW.title, to_jsonb(NEW));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS cerebro_strategy_item_history_trigger ON cerebro_strategy_item;
CREATE TRIGGER cerebro_strategy_item_history_trigger
AFTER INSERT OR UPDATE OR DELETE ON cerebro_strategy_item
FOR EACH ROW EXECUTE FUNCTION cerebro_record_strategy_item_history();
