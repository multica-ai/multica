DROP INDEX IF EXISTS idx_artifact_search_tsv;
DROP INDEX IF EXISTS idx_cerebro_note_comment_search_tsv;
DROP INDEX IF EXISTS idx_artifact_title_trgm;
DROP INDEX IF EXISTS idx_artifact_body_trgm;
DROP INDEX IF EXISTS idx_cerebro_note_comment_body_trgm;

ALTER TABLE artifact DROP COLUMN IF EXISTS search_tsv;
ALTER TABLE cerebro_note_comment DROP COLUMN IF EXISTS search_tsv;
