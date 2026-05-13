-- Rollback for 9023_cerebro_workflows_phase2_ext_pr3.up.sql.
--
-- Restores the post-9022 action_type list (without escalate_to_owner).
-- Operators must clear any rows that use the removed value first; the
-- ADD CONSTRAINT will fail otherwise and surface the offending row.

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
            'route_by_domain'
        )
    );
