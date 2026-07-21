DROP TABLE IF EXISTS gitlab_issue;
DROP INDEX IF EXISTS idx_comment_gitlab_note;
ALTER TABLE comment DROP COLUMN IF EXISTS gitlab_note_id;
