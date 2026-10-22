-- Extend issue.origin_type for issues created through ShareCRM's /issue
-- command. The shared channel Router stamps origin_type='sharecrm_chat'.
-- Same NOT VALID + separate VALIDATE pattern as Telegram (366/367).
-- The full list is respecified and must include every prior origin
-- (including telegram_chat from migration 366). Numbers 589/590 follow
-- upstream 588 (daemon token owner). Upstream took 585 for workflow owner,
-- 586 for cloud_agent_connection, 587 for its unique index, and 588 for
-- daemon_token.owner_id.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'sharecrm_chat'))
    NOT VALID;
