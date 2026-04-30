DROP INDEX IF EXISTS idx_comment_active_issue_created;
ALTER TABLE comment DROP COLUMN IF EXISTS deleted_at;
