-- GIN index on comment content for LIKE '%keyword%' queries (pg_bigm).
-- Only created when pg_bigm is installed.
DO $$
BEGIN
  CREATE INDEX idx_comment_content_bigm ON comment USING gin (content gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram index on comment (pg_bigm not installed)';
END
$$;
