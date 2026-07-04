-- Fallback GIN search indexes using pg_trgm for environments where pg_bigm
-- is unavailable. Background:
--
-- Migration 032 (issue_search_index) and 033 (comment_search_index) both
-- wrap `CREATE EXTENSION pg_bigm` and their GIN index creation in
-- EXCEPTION handlers, silently skipping if pg_bigm is not installed. This
-- was intended as a CI guardrail (see migration 032 header) so tests could
-- run against a stock Postgres image.
--
-- The unintended consequence: the bundled self-host / dev / CI Postgres
-- image is `pgvector/pgvector:pg17`, which does NOT ship pg_bigm. On every
-- self-hosted deployment the two migrations silently no-op, no GIN indexes
-- get built, and search queries fall back to a Seq Scan on `issue` +
-- correlated Seq Scans on `comment`. On a workspace with a few thousand
-- issues + tens of thousands of comments this manifests as the "search
-- freezes with no response" symptom reported in MUL-4059 — the query
-- runs long enough that the client either aborts or the operator's
-- reverse-proxy read timeout fires.
--
-- pg_trgm is part of PostgreSQL's contrib package and is shipped in every
-- image we support, including pgvector. It supports the same
-- `LOWER(col) LIKE '%pattern%'` pattern via GIN + gin_trgm_ops. For CJK
-- content pg_bigm remains the better choice (bigrams vs trigrams — a
-- 2-character CJK query indexes cleanly under pg_bigm but degrades to a
-- Seq Scan under pg_trgm) so we do NOT drop the pg_bigm indexes; the two
-- coexist and the planner picks whichever is cheaper.
--
-- Guardrails:
--   1. `CREATE EXTENSION IF NOT EXISTS pg_trgm` — safe on all supported
--      images. Wrapped in DO/EXCEPTION so a hypothetical minimal image
--      without pg_trgm still lets migrations finish (mirrors migration
--      032 pg_bigm pattern).
--   2. `CREATE INDEX IF NOT EXISTS` — idempotent on re-run.
--   3. Same LOWER() expression signature as the search handler's
--      `LOWER(i.title) LIKE $2` / `LOWER(COALESCE(i.description, '')) LIKE $2`
--      / `LOWER(c.content) LIKE $2` so the planner can match the index.
--
-- Lock impact: plain CREATE INDEX takes an AccessExclusive lock on the
-- table for the duration of the build. Cannot use CREATE INDEX
-- CONCURRENTLY here because it is disallowed inside a DO block (and
-- the extension-optional guardrail requires DO). Operators running very
-- large tables who want the concurrent variant can pre-create the three
-- indexes manually with CONCURRENTLY before applying the migration; the
-- IF NOT EXISTS check will then turn this migration into a no-op.

DO $$
BEGIN
  CREATE EXTENSION IF NOT EXISTS pg_trgm;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'pg_trgm not available, skipping trigram search indexes';
END
$$;

DO $$
BEGIN
  CREATE INDEX IF NOT EXISTS idx_issue_title_trgm
    ON issue USING gin (LOWER(title) gin_trgm_ops);
  CREATE INDEX IF NOT EXISTS idx_issue_description_trgm
    ON issue USING gin (LOWER(COALESCE(description, '')) gin_trgm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping trigram indexes on issue (pg_trgm not installed)';
END
$$;

DO $$
BEGIN
  CREATE INDEX IF NOT EXISTS idx_comment_content_trgm
    ON comment USING gin (LOWER(content) gin_trgm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping trigram index on comment (pg_trgm not installed)';
END
$$;
