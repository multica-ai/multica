-- FIR-2666: project sprint feature.
--
-- Four tables back the feature, all in the cerebro 9xxx namespace so the
-- upstream issue/project schemas stay untouched.
--
--   cerebro_sprint_settings    — per-project sprint configuration. Existence of
--                                a row (with enabled=TRUE) marks the project
--                                as a sprint container.
--   cerebro_sprint             — one row per sprint instance.
--   cerebro_sprint_issue       — issue ↔ sprint join (one sprint per issue).
--                                Kept separate so we never patch issue.*.
--   cerebro_sprint_recurring_task — fixed/recurring task template that gets
--                                cloned into every new sprint of a matching
--                                cadence.
--
-- Hard rule (Jesper): every periodic value (duration, lead-days, cadence) is
-- a setting on these tables — there are no constants in the runtime code.
--
-- Idempotent (CREATE IF NOT EXISTS / unique-constraint guards) so the
-- migration test's re-run pass is a no-op.

CREATE TABLE IF NOT EXISTS cerebro_sprint_settings (
    project_id              UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    workspace_id            UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    enabled                 BOOLEAN NOT NULL DEFAULT TRUE,

    -- Sprint length. unit ∈ {day,week,month}; count > 0.
    duration_unit           TEXT NOT NULL DEFAULT 'week'
        CHECK (duration_unit IN ('day', 'week', 'month')),
    duration_count          INTEGER NOT NULL DEFAULT 2
        CHECK (duration_count > 0),

    -- ISO weekday the first day of each sprint should fall on (1=Mon..7=Sun).
    -- Only meaningful for week-based sprints; month-based sprints ignore it.
    start_weekday           SMALLINT NOT NULL DEFAULT 1
        CHECK (start_weekday BETWEEN 1 AND 7),

    -- Human name template. {n} is replaced with sequence_no.
    name_template           TEXT NOT NULL DEFAULT 'Sprint {n}',

    -- Auto-create the next sprint when the active one is within
    -- auto_create_lead_days of its end. Both values are settings — there is
    -- no hardcoded "2" anywhere in the runtime.
    auto_create_enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    auto_create_lead_days   INTEGER NOT NULL DEFAULT 2
        CHECK (auto_create_lead_days >= 0),

    -- When TRUE the sweeper moves issues that are not in a terminal status
    -- (cancelled/done) from the closing sprint to the new sprint.
    move_incomplete_enabled BOOLEAN NOT NULL DEFAULT TRUE,

    -- Timezone used to interpret start_date/end_date as wall-clock days.
    -- Default UTC so an unconfigured workspace still produces consistent
    -- boundaries; admin can override per-project.
    timezone                TEXT NOT NULL DEFAULT 'UTC',

    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_sprint_settings_workspace
    ON cerebro_sprint_settings (workspace_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_sprint_settings_auto_create
    ON cerebro_sprint_settings (auto_create_enabled, enabled)
    WHERE auto_create_enabled = TRUE AND enabled = TRUE;

CREATE TABLE IF NOT EXISTS cerebro_sprint (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    sequence_no  INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'active', 'done', 'cancelled')),
    start_date   DATE NOT NULL,
    end_date     DATE NOT NULL,
    goal         TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_date >= start_date),
    CONSTRAINT cerebro_sprint_project_sequence_unique
        UNIQUE (project_id, sequence_no),
    CONSTRAINT cerebro_sprint_project_start_unique
        UNIQUE (project_id, start_date)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_sprint_project_status
    ON cerebro_sprint (project_id, status);
CREATE INDEX IF NOT EXISTS idx_cerebro_sprint_workspace
    ON cerebro_sprint (workspace_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_sprint_end_date
    ON cerebro_sprint (end_date)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS cerebro_sprint_issue (
    issue_id   UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    sprint_id  UUID NOT NULL REFERENCES cerebro_sprint(id) ON DELETE CASCADE,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_sprint_issue_sprint
    ON cerebro_sprint_issue (sprint_id);

CREATE TABLE IF NOT EXISTS cerebro_sprint_recurring_task (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id      UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,

    -- Cadence the template applies to. A new sprint clones the templates
    -- whose (cadence_unit, cadence_count) matches the project's current
    -- (duration_unit, duration_count) — that's how a "monthly" template
    -- rolls into every monthly sprint but stays out of weekly ones.
    cadence_unit    TEXT NOT NULL
        CHECK (cadence_unit IN ('day', 'week', 'month')),
    cadence_count   INTEGER NOT NULL CHECK (cadence_count > 0),

    title           TEXT NOT NULL,
    description     TEXT,
    priority        TEXT,

    assignee_type   TEXT CHECK (assignee_type IN ('member', 'agent')),
    assignee_id     UUID,

    enabled         BOOLEAN NOT NULL DEFAULT TRUE,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((assignee_type IS NULL) = (assignee_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_cerebro_sprint_recurring_task_project
    ON cerebro_sprint_recurring_task (project_id, enabled);
