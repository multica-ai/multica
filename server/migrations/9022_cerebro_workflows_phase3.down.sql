-- Rollback for 9022_cerebro_workflows_phase3.up.sql.
--
-- Restores the phase-2 trigger/action CHECK lists, drops the cron-state
-- table, the partial unique index on the inbound token, and the three new
-- columns. Rows that used a phase-3 trigger_type or action_type would prevent
-- the CHECK re-add — operators are expected to clear those before rolling
-- back (or accept the constraint failure as a deliberate guardrail).

DROP TABLE IF EXISTS cerebro_workflow_cron_state;

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_action_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_action_known CHECK (
        action_type IN (
            'set_status',
            'create_sub_issue',
            'send_reminder',
            'run_skill',
            'comment_on_issue'
        )
    );

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_trigger_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_trigger_known CHECK (
        trigger_type IN (
            'status_changed',
            'due_date_reached',
            'due_time_reached'
        )
    );

DROP INDEX IF EXISTS uq_cerebro_workflow_inbound_token;

ALTER TABLE cerebro_workflow
    DROP COLUMN IF EXISTS outbound_webhook_secret;

ALTER TABLE cerebro_workflow
    DROP COLUMN IF EXISTS inbound_signing_secret;

ALTER TABLE cerebro_workflow
    DROP COLUMN IF EXISTS inbound_webhook_token;
