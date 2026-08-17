-- Revert to the pre-wecom_chat issue_origin_type_check list. This restores the
-- state left by the newest earlier issue_origin_* migration, which includes
-- 'dingtalk_chat' (259) — dropping it here would break DingTalk.
--
-- The down path deliberately VALIDATEs in this same file, unlike the up path.
-- Narrowing a CHECK can genuinely be violated by existing data, so this must
-- fail closed while wecom_chat rows remain rather than silently leaving rows
-- the constraint forbids. That means accepting the ACCESS EXCLUSIVE lock for
-- the scan — an acceptable trade for an operator-driven rollback, which is
-- rare and supervised, where a silently untrusted constraint would be worse.
-- Same reasoning as 259_issue_origin_dingtalk_chat.down.sql.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat'))
    NOT VALID;
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
