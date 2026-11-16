-- Revert to the pre-agent_create issue_origin_type_check list. Any existing rows
-- with origin_type='agent_create' would violate the rolled-back constraint; the
-- down migration assumes the operator has already deleted or relabeled those
-- rows. Kept strict (no DROP NOT VALID dance) to preserve the schema invariant
-- downstream code relies on. Mirrors 131 (slack_chat).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat'));
