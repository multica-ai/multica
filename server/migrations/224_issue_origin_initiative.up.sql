-- Extend issue.origin_type so the initiative reconciler can stamp dispatched
-- issues with origin_type='initiative' + origin_id=<initiative_task.id>. The
-- origin link is the crash-recovery key: a reconcile pass that died between
-- issue creation and initiative_task.issue_id stamping re-adopts the issue via
-- GetIssueByOrigin instead of creating a duplicate. Mirrors the quick_create
-- (060) and agent_create (149) extensions.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'initiative'));
