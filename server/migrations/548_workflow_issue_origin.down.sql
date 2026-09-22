UPDATE issue SET origin_type=NULL, origin_id=NULL WHERE origin_type='workflow';
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat')) NOT VALID;
