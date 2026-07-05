-- Rolling back: rows created with origin_type='dingtalk_chat' would violate
-- the narrowed constraint; the down migration relabels them to 'quick_create'
-- (the neutral manual origin) before restoring the previous CHECK — the same
-- convention as 131's down migration.
UPDATE issue SET origin_type = 'quick_create' WHERE origin_type = 'dingtalk_chat';
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat'));
