-- Revert to the pre-wecom_chat issue_origin_type_check list. Any existing rows
-- with origin_type='wecom_chat' would violate the rolled-back constraint; the
-- rollback is safe only when no wecom-created issues exist.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create'));
