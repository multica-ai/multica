-- Revert to the pre-DingTalk origin list. Existing dingtalk_chat rows must be
-- deleted or relabeled before this rollback can succeed.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create'));
