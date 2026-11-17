-- Extend issue.origin_type for issues created by the WeChat `/issue`
-- command. origin_id stores the chat_session id, matching the existing Lark,
-- Slack, DingTalk, and WeCom channel origins.
--
-- This only widens the allowed set. Recreate the CHECK as NOT VALID so the
-- ACCESS EXCLUSIVE lock is held briefly; migration 668 performs the table scan
-- under SHARE UPDATE EXCLUSIVE without blocking normal reads and writes.
--
-- Fork: ShareCRM databases already allow origin_type 'sharecrm_chat' (old
-- migrations 665/666, now 673/674). Those rows exist before this file runs
-- on upgrade. Keep sharecrm_chat in the replacement check so migration 668
-- can validate them. Dropping it aborts the upgrade before 673 runs.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat'))
    NOT VALID;
