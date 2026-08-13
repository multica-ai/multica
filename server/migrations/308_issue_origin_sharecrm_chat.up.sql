-- Extend issue.origin_type for issues created through ShareCRM's /issue
-- command. The shared channel Router stamps origin_type='sharecrm_chat'.
-- Same NOT VALID + separate VALIDATE pattern as migration 259/260 and 263/264.
-- The full list is respecified and must include every prior origin (including
-- wecom_chat from migration 263). Numbers 308/309 follow main's 307.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'sharecrm_chat'))
    NOT VALID;
