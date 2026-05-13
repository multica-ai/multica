DROP INDEX IF EXISTS idx_attachment_classification;
DROP INDEX IF EXISTS idx_comment_classification;
DROP INDEX IF EXISTS idx_issue_classification;
ALTER TABLE attachment DROP COLUMN IF EXISTS classification;
ALTER TABLE comment DROP COLUMN IF EXISTS classification;
ALTER TABLE issue DROP COLUMN IF EXISTS classification;
