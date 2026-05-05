-- CEREBRO-PATCH(migration-idempotent-033-comment-search-index): cerebro modification of upstream file
-- GIN index on comment content for LIKE '%keyword%' queries (pg_bigm).
-- Only created when pg_bigm is installed.
DO $$
BEGIN
  CREATE INDEX IF NOT EXISTS idx_comment_content_bigm ON comment USING gin (content gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram index on comment (pg_bigm not installed)';
END
$$;
