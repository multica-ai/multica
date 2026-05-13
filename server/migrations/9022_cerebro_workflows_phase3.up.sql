-- Cerebro workflow engine, phase 3 (JEH-1108, PR 1 of 3).
--
-- Schema-level additions on top of 9021:
--
--   * inbound_webhook_token   TEXT — opaque per-workflow token used by the
--                                    public webhook-inbound endpoint (PR 2).
--                                    Partial-unique on (workspace_id, token)
--                                    so two workflows in the same workspace
--                                    cannot share one. NULL until the user
--                                    explicitly regenerates a token in the UI.
--   * inbound_signing_secret  TEXT — optional HMAC-armored secret. NULL means
--                                    "nem-mode": the token alone authorizes
--                                    the call. Setting it switches the
--                                    endpoint into HMAC-verified mode.
--   * outbound_webhook_secret TEXT — optional secret signed into the outbound
--                                    HTTP delivery (PR 2 wires the worker).
--
-- The trigger and action CHECK constraints are widened to include the new
-- phase-3 catalog entries:
--
--   trigger_type += ('cron', 'webhook_inbound', 'comment_mention',
--                    'all_children_done', 'sub_issue_created')
--   action_type  += ('webhook_outbound', 'reassign_issue')
--
-- cerebro_workflow_cron_state stores the per-workflow scheduler bookkeeping
-- (last fire timestamp + computed next fire). The sweeper that consumes it
-- ships in PR 2; this PR ships the table so the schema review and rollback
-- story stay together with the column additions.
--
-- All idempotent (ADD COLUMN IF NOT EXISTS / DO-block guards / CREATE TABLE
-- IF NOT EXISTS) so TestMigrationsAreIdempotent's re-run pass is a no-op.

ALTER TABLE cerebro_workflow
    ADD COLUMN IF NOT EXISTS inbound_webhook_token TEXT,
    ADD COLUMN IF NOT EXISTS inbound_signing_secret TEXT,
    ADD COLUMN IF NOT EXISTS outbound_webhook_secret TEXT;

-- Per-workspace uniqueness for the inbound token. Partial index — only rows
-- with a non-null token participate, so the column can remain NULL for
-- workflows that have never had a token generated.
CREATE UNIQUE INDEX IF NOT EXISTS uq_cerebro_workflow_inbound_token
    ON cerebro_workflow (workspace_id, inbound_webhook_token)
    WHERE inbound_webhook_token IS NOT NULL;

-- Replace the trigger-type CHECK with the phase-3 expanded list. DROP + ADD
-- is the only portable way to widen a CHECK; doing it in one statement keeps
-- the table validation gap as small as possible.
ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_trigger_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_trigger_known CHECK (
        trigger_type IN (
            'status_changed',
            'due_date_reached',
            'due_time_reached',
            'cron',
            'webhook_inbound',
            'comment_mention',
            'all_children_done',
            'sub_issue_created'
        )
    );

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_action_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_action_known CHECK (
        action_type IN (
            'set_status',
            'create_sub_issue',
            'send_reminder',
            'run_skill',
            'comment_on_issue',
            'webhook_outbound',
            'reassign_issue'
        )
    );

-- Per-workflow cron scheduler bookkeeping. The PR-2 sweeper updates this on
-- every tick: marks last_fired_at when it dispatches the trigger event, and
-- recomputes next_fire_at from the workflow's ScheduleExpr (stored in the
-- workflow's trigger_config). One row per workflow; no row means the cron
-- has never run yet (sweeper inserts on first evaluation).
CREATE TABLE IF NOT EXISTS cerebro_workflow_cron_state (
    workflow_id    UUID PRIMARY KEY REFERENCES cerebro_workflow(id) ON DELETE CASCADE,
    last_fired_at  TIMESTAMPTZ,
    next_fire_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_cron_state_next_fire
    ON cerebro_workflow_cron_state (next_fire_at)
    WHERE next_fire_at IS NOT NULL;
