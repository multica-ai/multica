-- Rollback for 9021_cerebro_workflows_phase2.up.sql.
--
-- Restores the phase-1 action-type CHECK list and drops the two phase-2
-- editor columns. Rows that used a phase-2 action_type would already have
-- prevented this — operators are expected to clear those before rolling back
-- (or accept the constraint failure as a deliberate guardrail).

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_action_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_action_known CHECK (
        action_type IN (
            'set_status',
            'create_sub_issue',
            'send_reminder'
        )
    );

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_editor_mode_known;

ALTER TABLE cerebro_workflow
    DROP COLUMN IF EXISTS editor_layout;

ALTER TABLE cerebro_workflow
    DROP COLUMN IF EXISTS editor_mode;
