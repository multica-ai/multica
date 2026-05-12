-- Cerebro workflow engine, phase 1 (JEH-1047, PR 1 of 3).
--
-- Four tables back the engine:
--
--   cerebro_workflow                — rules: trigger, conditions, action (JSON
--                                     payloads), enabled, ownership, scope to
--                                     workspace and optional project.
--   cerebro_workflow_run            — one row per execution attempt; carries
--                                     the link from the agent_task_queue row
--                                     it spawned (task_id) back to the rule
--                                     so we never have to add columns to the
--                                     upstream agent_task_queue table.
--   cerebro_workflow_idempotency_key — hash(event_id || action_id) → already-
--                                     handled marker. Prevents double-fires
--                                     when the same domain event is replayed.
--   cerebro_issue_due_time          — optional per-issue clock override (path
--                                     B in the spec). Joined by the due-time
--                                     sweeper with issue.due_date. Per-issue
--                                     UI lands in a later phase; the table
--                                     ships now so the sweeper has somewhere
--                                     to read from on day one.
--
-- All idempotent (CREATE IF NOT EXISTS / unique-constraint guards) so
-- TestMigrationsAreIdempotent's re-run pass is a no-op. Feature-flagged at
-- runtime via cerebro_workflows; tables stay empty until the flag is on.

CREATE TABLE IF NOT EXISTS cerebro_workflow (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id     UUID REFERENCES project(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,

    -- Trigger half of the rule.
    trigger_type   TEXT NOT NULL,
    trigger_config JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Optional condition filter applied after the trigger matches.
    conditions     JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- Single action per workflow in phase 1. Multi-action graphs land in
    -- phase 2 when we ship the node-canvas builder.
    action_type    TEXT NOT NULL,
    action_config  JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_by_id   UUID NOT NULL,
    created_by_type TEXT NOT NULL
        CHECK (created_by_type IN ('member', 'agent')),

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cerebro_workflow_trigger_known CHECK (
        trigger_type IN (
            'status_changed',
            'due_date_reached',
            'due_time_reached'
        )
    ),
    CONSTRAINT cerebro_workflow_action_known CHECK (
        action_type IN (
            'set_status',
            'create_sub_issue',
            'send_reminder'
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_workspace_enabled
    ON cerebro_workflow (workspace_id, enabled);
CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_workspace_trigger
    ON cerebro_workflow (workspace_id, trigger_type) WHERE enabled = TRUE;

CREATE TABLE IF NOT EXISTS cerebro_workflow_run (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id     UUID NOT NULL REFERENCES cerebro_workflow(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,

    -- Free-form snapshot of the domain event that fired this run. Stored as
    -- JSON so we don't depend on the event-bus payload shape evolving in
    -- lock-step with this engine.
    trigger_event   JSONB NOT NULL,

    -- The issue the trigger operated on (parent of any sub-issue an action
    -- subsequently created). Nullable for trigger types that don't bind to
    -- a specific issue (cron, etc.).
    target_issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,

    -- Set when an action created an agent_task_queue row. Keeps the upstream
    -- agent_task_queue schema untouched.
    task_id         UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,

    status          TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'success', 'failed', 'escalated')),
    attempt         INT NOT NULL DEFAULT 1,
    error           TEXT,

    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    next_retry_at   TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_run_workflow_created
    ON cerebro_workflow_run (workflow_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_run_pending_retry
    ON cerebro_workflow_run (next_retry_at)
    WHERE status = 'failed' AND next_retry_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_run_workspace_created
    ON cerebro_workflow_run (workspace_id, created_at DESC);

-- Idempotency. Key = hash(event_id || action_id) constructed by the caller.
-- TEXT (not UUID) because the hash is a hex digest, and stable across
-- restarts. A unique row per (workflow_id, key) prevents duplicate execution
-- when the same event is replayed.
CREATE TABLE IF NOT EXISTS cerebro_workflow_idempotency_key (
    key          TEXT NOT NULL,
    workflow_id  UUID NOT NULL REFERENCES cerebro_workflow(id) ON DELETE CASCADE,
    run_id       UUID REFERENCES cerebro_workflow_run(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workflow_id, key)
);

-- TTL helper: index supports the eventual cleanup sweeper that prunes rows
-- older than the chosen retention window.
CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_idempotency_key_created
    ON cerebro_workflow_idempotency_key (created_at);

-- Path-B per-issue clock override. The due-time sweeper joins
-- issue.due_date (upstream) with this table. Empty by default; only rows
-- where a user (or workflow) explicitly set a clock time exist.
CREATE TABLE IF NOT EXISTS cerebro_issue_due_time (
    issue_id     UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    due_at       TIMESTAMPTZ NOT NULL,
    set_by_id    UUID NOT NULL,
    set_by_type  TEXT NOT NULL
        CHECK (set_by_type IN ('member', 'agent', 'workflow')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_issue_due_time_due_at
    ON cerebro_issue_due_time (due_at);
