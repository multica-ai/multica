-- FIR-3421: configurable meetings and accountability seats.

CREATE TABLE IF NOT EXISTS cerebro_operating_meeting (
    workspace_id UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    note_type_id UUID REFERENCES cerebro_note_type(id) ON DELETE SET NULL,
    cadence_unit TEXT NOT NULL DEFAULT 'manual'
        CHECK (cadence_unit IN ('manual', 'day', 'week', 'month', 'quarter')),
    cadence_count INTEGER NOT NULL DEFAULT 1 CHECK (cadence_count > 0),
    agenda JSONB NOT NULL DEFAULT '[
        {"id":"check-in","name":"Check-in","position":0,"binding":"none"},
        {"id":"scorecard-review","name":"Scorecard review","position":1,"binding":"scorecard"},
        {"id":"goal-review","name":"Goal review","position":2,"binding":"goals"},
        {"id":"headlines","name":"Headlines","position":3,"binding":"none"},
        {"id":"todos","name":"Todos","position":4,"binding":"none"},
        {"id":"issue-solving","name":"Issue solving","position":5,"binding":"issues_list"},
        {"id":"conclude","name":"Conclude","position":6,"binding":"none"}
    ]'::jsonb CHECK (jsonb_typeof(agenda) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cerebro_org_chart_seat (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES cerebro_org_chart_seat(id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (btrim(name) <> ''),
    responsibilities JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(responsibilities) = 'array'),
    owner_type TEXT CHECK (owner_type IS NULL OR owner_type IN ('member', 'agent')),
    owner_id UUID,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((owner_type IS NULL) = (owner_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_cerebro_org_chart_seat_workspace_parent_position
    ON cerebro_org_chart_seat (workspace_id, parent_id, position, created_at);
