DROP INDEX IF EXISTS idx_issue_search_tsv;
DROP INDEX IF EXISTS idx_comment_search_tsv;
DROP INDEX IF EXISTS idx_issue_title_trgm;
DROP INDEX IF EXISTS idx_issue_description_trgm;
DROP INDEX IF EXISTS idx_comment_content_trgm;

ALTER TABLE issue DROP COLUMN IF EXISTS search_tsv;
ALTER TABLE comment DROP COLUMN IF EXISTS search_tsv;
