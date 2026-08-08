-- Allow /issue commands received from the personal Weixin channel. The CHECK
-- is widened without an inline table scan; migration 266 validates it under a
-- weaker lock.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'weixin_chat'))
    NOT VALID;
