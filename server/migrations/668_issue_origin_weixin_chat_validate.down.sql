-- PostgreSQL cannot mark a validated constraint NOT VALID. Recreate the state
-- immediately after migration 667: enforcing new writes but not yet validated.
-- sharecrm_chat stays, matching 667, so a partial rollback does not drop it.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'weixin_chat', 'sharecrm_chat'))
    NOT VALID;
