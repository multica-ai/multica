-- Revert to the pre-wecom_chat issue_origin_type_check list. This restores the
-- state left by the newest earlier issue_origin_* migration, which includes
-- 'dingtalk_chat' (259) — dropping it here would break DingTalk. Any existing
-- rows with origin_type='wecom_chat' would violate the rolled-back constraint;
-- the rollback is safe only when no wecom-created issues exist.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat'));
