-- Extend issue.origin_type to allow the inbound Jira webhook to stamp issues
-- mirrored from Jira with origin_type='jira' + origin_id=<jira_connection.id>.
-- Mirrors migrations 060/111/131/149, which extended the same constraint for
-- quick_create / lark_chat / slack_chat / agent_create.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'jira'));
