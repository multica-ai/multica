ALTER TABLE daily_plan
    ADD COLUMN IF NOT EXISTS energy_level INT CHECK (energy_level BETWEEN 1 AND 5),
    ADD COLUMN IF NOT EXISTS energy_note TEXT,
    ADD COLUMN IF NOT EXISTS recovery_need BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS capacity_minutes INT,
    ADD COLUMN IF NOT EXISTS capacity_note TEXT;

CREATE TABLE IF NOT EXISTS plan_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    plan_id UUID NOT NULL REFERENCES daily_plan(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    suggested_issue_type_id UUID REFERENCES issue_type(id) ON DELETE SET NULL,
    title_snapshot TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    position INT NOT NULL,
    estimated_minutes INT,
    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'in_progress', 'progressed', 'done', 'skipped')),
    status_reason TEXT,
    source TEXT NOT NULL DEFAULT 'manual'
        CHECK (source IN ('manual', 'generated', 'carry_over')),
    completed_at TIMESTAMPTZ,
    skipped_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_plan_item_plan_position
    ON plan_item (plan_id, position);

CREATE INDEX IF NOT EXISTS idx_plan_item_issue
    ON plan_item (workspace_id, issue_id);

CREATE INDEX IF NOT EXISTS idx_plan_item_user_status
    ON plan_item (workspace_id, user_id, status);

CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_item_unique_plan_issue
    ON plan_item (plan_id, issue_id)
    WHERE issue_id IS NOT NULL;

ALTER TABLE focus_sessions
    ADD COLUMN IF NOT EXISTS plan_item_id UUID REFERENCES plan_item(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_focus_sessions_plan_item
    ON focus_sessions (workspace_id, plan_item_id);

ALTER TABLE time_entry
    ADD COLUMN IF NOT EXISTS plan_item_id UUID REFERENCES plan_item(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_time_entry_plan_item
    ON time_entry (workspace_id, plan_item_id);
