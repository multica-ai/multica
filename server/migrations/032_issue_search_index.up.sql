-- Enable pg_bigm extension for bigram-based full-text search (CJK-friendly).
-- Skips gracefully if pg_bigm is not available (e.g. CI environments).
DO $$
BEGIN
  CREATE EXTENSION IF NOT EXISTS pg_bigm;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'pg_bigm not available, skipping bigram indexes';
END
$$;

-- GIN indexes on issue title/description for LIKE '%keyword%' queries.
-- Only created when pg_bigm is installed.
DO $$
BEGIN
  CREATE INDEX idx_issue_title_bigm ON issue USING gin (title gin_bigm_ops);
  CREATE INDEX idx_issue_description_bigm ON issue USING gin (COALESCE(description, '') gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram indexes (pg_bigm not installed)';
END
$$;
