-- TECH-3064 / FIR-334: recurring issues.
--
-- Lets a user mark an individual issue as recurring: when the issue reaches
-- its trigger status (default 'done'), the server spawns the next occurrence
-- with a fresh due date and the same data (assignee, text, labels,
-- attachments), and chains the recurrence rule onto the new issue.
--
-- Built in the cerebro 9xxx namespace so the upstream issue schema stays
-- untouched. The recurrence rule lives on its own table keyed to the issue
-- it currently rides on; the engine is a poll-based sweeper that mirrors the
-- sprint sweeper (server/internal/cerebro/sprints/sweeper.go).
--
--   cerebro_issue_recurrence       — one rule per source issue.
--   cerebro_issue_recurrence_log   — audit row per spawned occurrence.
--
-- Idempotent (CREATE IF NOT EXISTS) so the migration test's re-run pass is a
-- no-op.

CREATE TABLE IF NOT EXISTS cerebro_issue_recurrence (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id      UUID REFERENCES project(id) ON DELETE SET NULL,

    -- The issue the rule currently rides on. When create_new_issue spawns
    -- the next occurrence this is repointed to the new issue, so the chain
    -- continues. One active rule per issue.
    source_issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,

    -- How the next due date is derived. unit ∈ {daily,weekly,monthly,yearly,
    -- days_after,custom}; interval_count is the "every N". weekdays holds ISO
    -- weekdays (1=Mon..7=Sun) for weekly/custom; days_after holds the offset
    -- for the days_after frequency.
    frequency       TEXT NOT NULL DEFAULT 'weekly'
        CHECK (frequency IN ('daily', 'weekly', 'monthly', 'yearly', 'days_after', 'custom')),
    interval_count  INTEGER NOT NULL DEFAULT 1 CHECK (interval_count > 0),
    weekdays        SMALLINT[] NOT NULL DEFAULT '{}',
    days_after      INTEGER NOT NULL DEFAULT 0 CHECK (days_after >= 0),

    -- The status that triggers the next occurrence (mockup: "On status
    -- change: Closed"). Default 'done'.
    trigger_status  TEXT NOT NULL DEFAULT 'done',

    -- Where the new due date is anchored: 'completion' = from the day the
    -- issue was completed; 'due_date' = from the previous due date
    -- (mockup: "Sync recurrence to due date").
    anchor          TEXT NOT NULL DEFAULT 'completion'
        CHECK (anchor IN ('completion', 'due_date')),

    -- create_new_issue=TRUE clones a fresh issue (mockup "Create new task");
    -- FALSE reopens the same issue into new_status with a bumped due date.
    create_new_issue BOOLEAN NOT NULL DEFAULT TRUE,
    -- Status the spawned/reopened issue lands in (mockup: "Update status to:
    -- TO DO").
    new_status      TEXT NOT NULL DEFAULT 'todo',

    -- Stop conditions. recur_forever=TRUE ignores end_date/max_occurrences.
    recur_forever   BOOLEAN NOT NULL DEFAULT TRUE,
    end_date        DATE,
    max_occurrences INTEGER CHECK (max_occurrences IS NULL OR max_occurrences > 0),
    occurrence_count INTEGER NOT NULL DEFAULT 0,

    -- Edge-trigger guard for the poll-based sweeper: armed flips to TRUE
    -- while the source issue is NOT in trigger_status, and a fire is only
    -- allowed when armed=TRUE and the issue is in trigger_status. Prevents
    -- the sweeper from re-firing on every tick while an issue sits closed.
    armed           BOOLEAN NOT NULL DEFAULT TRUE,

    enabled         BOOLEAN NOT NULL DEFAULT TRUE,

    created_by_type TEXT CHECK (created_by_type IN ('member', 'agent')),
    created_by_id   UUID,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_issue_recurrence_source_unique UNIQUE (source_issue_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_issue_recurrence_workspace
    ON cerebro_issue_recurrence (workspace_id);
CREATE INDEX IF NOT EXISTS idx_cerebro_issue_recurrence_enabled
    ON cerebro_issue_recurrence (enabled)
    WHERE enabled = TRUE;

CREATE TABLE IF NOT EXISTS cerebro_issue_recurrence_log (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recurrence_id     UUID NOT NULL REFERENCES cerebro_issue_recurrence(id) ON DELETE CASCADE,
    -- The issue whose closure triggered this occurrence.
    trigger_issue_id  UUID REFERENCES issue(id) ON DELETE SET NULL,
    -- The newly created issue (NULL when create_new_issue=FALSE / reopen).
    spawned_issue_id  UUID REFERENCES issue(id) ON DELETE SET NULL,
    occurrence_number INTEGER NOT NULL,
    spawned_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_issue_recurrence_log_recurrence
    ON cerebro_issue_recurrence_log (recurrence_id, occurrence_number);
