-- Revert handoff records before narrowing the constraint, or the ADD fails.
DELETE FROM comment WHERE type = 'handoff';
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_type_check
    CHECK (type IN ('comment', 'status_change', 'progress_update', 'system'));
