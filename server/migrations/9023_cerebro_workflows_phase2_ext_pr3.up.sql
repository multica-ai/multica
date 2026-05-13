-- Cerebro workflow engine, phase 2 extension PR 3 (JEH-1114, PR 3 of 3).
--
-- Closes the JEH-1114 chain by adding the third primitive from the RFC:
--
--   escalate_to_owner — walks the parent_issue_id chain up from a stalled
--                       sub-issue and posts a backlinked comment on the
--                       first ancestor with a real assignee. Threshold is
--                       per-workflow (max_age_hours; default 12). Reuses
--                       the comment_on_issue write path.
--
-- Same pattern as 9022: drop + add the action_type CHECK rather than try
-- to widen it in place. Idempotent.

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
            'route_by_domain',
            'escalate_to_owner'
        )
    );
