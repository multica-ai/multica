-- Ship Hub Phase 3: card action chips.
--
-- Phase 1 + 2 made the Kanban observation-only. Phase 3 lets the user click
-- on a card to take an action — merge, comment, dispatch a smoke-test
-- workflow, spawn a "diagnose CI" agent task, etc. Each action call writes
-- a row here so the audit trail (who pressed what, what GitHub returned,
-- which agent task was spawned) is durable.
--
-- The table is intentionally narrow: action + payload + result. We avoid
-- separate columns per action variety so adding a new chip is a one-line
-- additive change in code rather than a schema migration. The action
-- column is open string (not enum) for the same reason — Phase 4's
-- "promote staging → production" chip can land without a CREATE TYPE.

CREATE TABLE ship_card_action (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    pull_request_id UUID NOT NULL REFERENCES pull_request(id) ON DELETE CASCADE,
    -- Nullable because system-actor automations (e.g. "auto-merge when CI
    -- passes" in Phase 4) won't carry a user id.
    actor_user_id   UUID REFERENCES "user"(id) ON DELETE SET NULL,
    -- One of: "merge" | "rebase_on_main" | "comment" | "dismiss_review" |
    -- "diagnose_ci_failure" | "summarize_review_feedback" | "nudge_author"
    -- | "run_smoke_tests" | "close_as_stale". Open string so a new chip
    -- doesn't require a CREATE TYPE migration.
    action          TEXT NOT NULL,
    -- Verbatim request body (post-redaction in code if needed). Audit
    -- consumers read this to reconstruct exactly what the user asked for.
    payload         JSONB,
    -- "succeeded" | "failed" | "in_progress". in_progress survives the
    -- request return for actions that spawn an async agent task; the task
    -- callback flips it to succeeded/failed.
    result_status   TEXT NOT NULL,
    -- Action-shape-specific payload: GitHub merge SHA, dispatched
    -- agent_task_id, error message, etc. Free-form so the per-action code
    -- decides what's worth keeping.
    result_payload  JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

-- Per-PR audit timeline (the most common read: "what's been done to this
-- card lately"). DESC because new actions sit at the top.
CREATE INDEX idx_ship_card_action_pr
    ON ship_card_action(pull_request_id, created_at DESC);

-- Workspace-scoped audit feed (admin reports + future "show all chip
-- presses" view).
CREATE INDEX idx_ship_card_action_workspace
    ON ship_card_action(workspace_id, created_at DESC);

-- Optional smoke-tests workflow file path. Set per workspace; the
-- run_smoke_tests chip dispatches workflow_dispatch on this file with
-- environment_id as input. Empty/NULL hides the chip from the UI.
ALTER TABLE workspace ADD COLUMN ship_hub_smoke_workflow TEXT;
