-- Add 'plan_request' to the allowed comment types
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_type_check
    CHECK (type IN ('comment', 'status_change', 'progress_update', 'system', 'plan_request'));
