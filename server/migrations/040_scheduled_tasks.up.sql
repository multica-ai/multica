-- Scheduled tasks: cron-like recipes that periodically enqueue an issue
-- assigned to an agent or member. The backend scheduler goroutine polls for
-- rows where next_run_at <= now(), creates the corresponding issue, and
-- advances next_run_at to the next cron slot.
--
-- From that point on the normal on_assign trigger takes over — the daemon
-- claims the issue and executes it. Nothing in the daemon or task queue
-- needs to be aware of scheduled tasks.
CREATE TABLE scheduled_task (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    created_by       UUID NOT NULL REFERENCES member(id) ON DELETE CASCADE,

    name             TEXT NOT NULL,
    title_template   TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',

    assignee_type    TEXT NOT NULL CHECK (assignee_type IN ('agent', 'member')),
    assignee_id      UUID NOT NULL,
    priority         TEXT NOT NULL DEFAULT 'none',

    cron_expression  TEXT NOT NULL,
    timezone         TEXT NOT NULL DEFAULT 'UTC',

    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at      TIMESTAMPTZ NOT NULL,
    last_run_at      TIMESTAMPTZ,
    last_run_issue_id UUID,
    last_run_error   TEXT,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at      TIMESTAMPTZ
);

-- Partial index used by the scheduler tick to efficiently find due rows.
CREATE INDEX idx_scheduled_task_due
    ON scheduled_task (next_run_at)
    WHERE enabled = TRUE AND archived_at IS NULL;

CREATE INDEX idx_scheduled_task_workspace
    ON scheduled_task (workspace_id)
    WHERE archived_at IS NULL;
