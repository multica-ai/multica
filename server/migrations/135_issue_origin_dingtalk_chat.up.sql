-- The DingTalk inbound channel creates issues from the /issue chat command
-- with origin_type='dingtalk_chat'. Same shape as 131 (slack_chat) and the
-- earlier lark_chat addition: widen the CHECK to accept the new label.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'dingtalk_chat'));
