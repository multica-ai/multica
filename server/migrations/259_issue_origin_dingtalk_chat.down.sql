-- Revert to the pre-DingTalk origin list. Existing dingtalk_chat rows must be
-- deleted or relabeled before this rollback can succeed.
--
-- The down path deliberately VALIDATEs in this same file, unlike the up path.
-- Narrowing a CHECK can genuinely be violated by existing data, so this must
-- fail closed while dingtalk_chat rows remain rather than silently leaving
-- rows the constraint forbids. That means accepting the ACCESS EXCLUSIVE lock
-- for the scan — an acceptable trade for an operator-driven rollback, which is
-- rare and supervised, where a silently untrusted constraint would be worse.
-- Same reasoning as 249_issue_subscriber_delegated.down.sql.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create'))
    NOT VALID;
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
