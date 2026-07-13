-- Existing issues must be cleared before the narrower constraint is restored.
UPDATE issue SET origin_type = NULL, origin_id = NULL
WHERE origin_type = 'note_comment';

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'runtime_approval', 'agent_task', 'lark_chat'));
