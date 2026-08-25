-- Extend issue.origin_type for issues created through ShareCRM's /issue
-- command. The shared channel Router stamps origin_type='sharecrm_chat'.
-- Same NOT VALID + separate VALIDATE pattern as Telegram (366/367).
-- The full list is respecified and must include every prior origin
-- (including telegram_chat from migration 366). Numbers 432/433 follow
-- main's 431 (channel chat route / task delivery / explicit origin backfill).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'sharecrm_chat'))
    NOT VALID;
