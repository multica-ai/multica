-- PostgreSQL cannot mark a validated constraint NOT VALID again. Recreate it
-- to restore the state immediately after migration 263 (present, enforcing new
-- writes, not yet validated). Mirrors
-- 260_issue_origin_dingtalk_chat_validate.down.sql.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat'))
    NOT VALID;
