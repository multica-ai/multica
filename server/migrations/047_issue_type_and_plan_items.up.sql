CREATE TABLE IF NOT EXISTS issue_type (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    color TEXT NOT NULL DEFAULT 'gray',
    icon TEXT NOT NULL DEFAULT 'circle',
    load_profile TEXT NOT NULL DEFAULT 'neutral'
        CHECK (load_profile IN ('deep_work', 'light_work', 'recovery', 'neutral')),
    is_system BOOLEAN NOT NULL DEFAULT false,
    archived_at TIMESTAMPTZ,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, key)
);

CREATE INDEX IF NOT EXISTS idx_issue_type_workspace_active
    ON issue_type (workspace_id, archived_at, position);

INSERT INTO issue_type (workspace_id, key, name, description, color, icon, load_profile, is_system, position)
SELECT w.id, seed.key, seed.name, seed.description, seed.color, seed.icon, seed.load_profile, true, seed.position
FROM workspace w
CROSS JOIN (
    VALUES
        ('task', 'Task', 'Default execution task.', 'slate', 'check-circle', 'neutral', 10),
        ('feature', 'Feature', 'Feature or delivery work.', 'blue', 'sparkles', 'deep_work', 20),
        ('bug', 'Bug', 'Defect investigation or fix.', 'red', 'bug', 'deep_work', 30),
        ('chore', 'Chore', 'Maintenance, cleanup, or operational work.', 'amber', 'wrench', 'light_work', 40),
        ('research', 'Research', 'Research, clarification, or exploration.', 'violet', 'search', 'light_work', 50),
        ('recovery', 'Recovery', 'Recovery, load reduction, or energy restoration.', 'emerald', 'battery', 'recovery', 60)
) AS seed(key, name, description, color, icon, load_profile, position)
ON CONFLICT (workspace_id, key) DO NOTHING;

ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS issue_type_id UUID REFERENCES issue_type(id) ON DELETE SET NULL;

UPDATE issue i
SET issue_type_id = t.id
FROM issue_type t
WHERE i.issue_type_id IS NULL
  AND t.workspace_id = i.workspace_id
  AND t.key = 'task';

CREATE INDEX IF NOT EXISTS idx_issue_workspace_type
    ON issue (workspace_id, issue_type_id);

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
