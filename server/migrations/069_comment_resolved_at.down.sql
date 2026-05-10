DROP INDEX IF EXISTS comment_issue_resolved_at_idx;
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_resolved_consistency;
ALTER TABLE comment
    DROP COLUMN IF EXISTS resolved_by_id,
    DROP COLUMN IF EXISTS resolved_by_type,
    DROP COLUMN IF EXISTS resolved_at;
