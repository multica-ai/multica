DROP INDEX IF EXISTS idx_cerebro_note_comment_issue;

ALTER TABLE cerebro_note_comment DROP COLUMN IF EXISTS issue_id;
