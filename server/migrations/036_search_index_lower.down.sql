-- Revert to original (non-LOWER) pg_bigm indexes.
DROP INDEX IF EXISTS idx_issue_title_bigm;
DROP INDEX IF EXISTS idx_issue_description_bigm;
DROP INDEX IF EXISTS idx_comment_content_bigm;

DO $$
BEGIN
  CREATE INDEX idx_issue_title_bigm ON issue USING gin (title gin_bigm_ops);
  CREATE INDEX idx_issue_description_bigm ON issue USING gin (COALESCE(description, '') gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram indexes on issue (pg_bigm not installed)';
END
$$;

DO $$
BEGIN
  CREATE INDEX idx_comment_content_bigm ON comment USING gin (content gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram index on comment (pg_bigm not installed)';
END
$$;
