-- Rollback for 9022_cerebro_workflows_phase2_ext.up.sql.
--
-- Restores the phase-2 action_type list (without route_by_domain). Operators
-- are expected to clear any rows that use the removed value before rolling
-- back; otherwise the ADD CONSTRAINT will fail and surface the offending row.

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
