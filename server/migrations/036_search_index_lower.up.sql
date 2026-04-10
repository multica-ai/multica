-- Rebuild pg_bigm GIN indexes on LOWER() expressions so that
-- LOWER(column) LIKE pattern queries can utilize them.
-- pg_bigm 1.2 (RDS) does not support ILIKE index scans;
-- LOWER(col) LIKE LOWER(pattern) is the compatible alternative.

-- Drop old indexes that were built on raw (non-lowered) columns.
DROP INDEX IF EXISTS idx_issue_title_bigm;
DROP INDEX IF EXISTS idx_issue_description_bigm;
DROP INDEX IF EXISTS idx_comment_content_bigm;

-- Recreate indexes on LOWER() expressions.
-- Wrapped in exception handler so CI environments without pg_bigm still pass.
DO $$
BEGIN
  CREATE INDEX idx_issue_title_bigm ON issue USING gin (LOWER(title) gin_bigm_ops);
  CREATE INDEX idx_issue_description_bigm ON issue USING gin (LOWER(COALESCE(description, '')) gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram indexes on issue (pg_bigm not installed)';
END
$$;

DO $$
BEGIN
  CREATE INDEX idx_comment_content_bigm ON comment USING gin (LOWER(content) gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram index on comment (pg_bigm not installed)';
END
$$;
