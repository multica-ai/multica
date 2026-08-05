-- Extend issue.origin_type for issues created through DingTalk's /issue
-- command. The shared channel Router stamps origin_type='dingtalk_chat' and
-- origin_id=<chat_session.id>; without this CHECK entry every DingTalk issue
-- creation fails with SQLSTATE 23514.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat'));
