-- Extend issue.origin_type for issues created through ShareCRM's /issue
-- command. The shared channel Router stamps origin_type='sharecrm_chat'.
-- Same NOT VALID + separate VALIDATE pattern as WeChat (667/668).
-- The full list is respecified and must include every prior origin,
-- including weixin_chat from migration 667. Numbers 673/674 follow
-- upstream 672 (weixin_account_state_primary_key).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat'))
    NOT VALID;
