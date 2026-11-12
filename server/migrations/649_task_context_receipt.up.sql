-- Per-run context receipt (MUL-9013): which context sources the daemon handed
-- a run, with sizes, short hashes, delivery and load evidence. Metadata only;
-- the daemon never sends source content. One row per task, replaced when the
-- daemon re-reports for a fresh-session retry.
--
-- No foreign keys (repo rule): workspace teardown (DeleteTaskBatch and the
-- workspace leaf sweep) and issue deletion remove these rows in application
-- code. issue_id is copied from the task so issue deletion can find them
-- after the legacy issue -> agent_task_queue cascade has run.
CREATE TABLE IF NOT EXISTS task_context_receipt (
    task_id      UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    issue_id     UUID,
    receipt      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
