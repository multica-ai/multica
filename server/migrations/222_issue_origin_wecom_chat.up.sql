-- Extend issue.origin_type to allow the WeChat Work smart-bot (aibot) `/issue`
-- command path to stamp issues with origin_type='wecom_chat' + origin_id=
-- <chat_session.id>. Mirrors 111_issue_origin_lark_chat and
-- 131_issue_origin_slack_chat — same origin_id semantics (the chat_session
-- the /issue command was typed in), different label because analytics and
-- inbound routing key on this string.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'wecom_chat'));
