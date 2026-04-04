DROP TRIGGER IF EXISTS trg_issue_search_vector ON issue;
DROP FUNCTION IF EXISTS issue_search_vector_update();
DROP INDEX IF EXISTS idx_issue_search;
ALTER TABLE issue DROP COLUMN IF EXISTS search_vector;
