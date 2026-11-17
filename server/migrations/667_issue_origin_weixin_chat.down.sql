-- Restore the validated pre-WeChat constraint. This intentionally fails
-- closed while weixin_chat rows remain, because a rollback must not leave a
-- trusted constraint that existing rows violate.
--
-- Fork: keep sharecrm_chat. Upgraded ShareCRM databases allowed it before
-- this migration, and rolling WeChat back must not invalidate those rows.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'sharecrm_chat'))
    NOT VALID;
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
