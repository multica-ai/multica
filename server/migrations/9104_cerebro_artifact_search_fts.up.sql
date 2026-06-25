-- FIR-2022 / note search — Postgres FTS over documents (artifacts) and their
-- comments, mirroring the issue-search pipeline from migration 9055.
--
-- A "note" is an artifact (kind='note') plus a cerebro_note row; "all documents"
-- (Jesper) means every artifact kind — report/plan/decision/diagram/note — can
-- be searched. Comments live in cerebro_note_comment, whose note_id FKs
-- artifact(id) since 9089, so one comment table covers notes AND plain docs.
--
-- This adds:
--   * artifact.search_tsv          — title (A) + body (B), GIN-indexed.
--   * cerebro_note_comment.search_tsv — body, GIN-indexed.
--   * pg_trgm indexes on title/body/comment-body for spell-tolerance, matching
--     the `<%` operator the query builder uses (cerebro/note/search.go).
--
-- Configuration "simple" keeps tokenisation language-neutral (notes mix Danish
-- and English), exactly like the issue FTS. The tsvector columns are GENERATED
-- ALWAYS STORED, so writes keep them in sync with no trigger to maintain. CI
-- environments without pg_trgm skip the trigram indexes silently so the gate
-- stays green.

DO $$
BEGIN
  CREATE EXTENSION IF NOT EXISTS pg_trgm;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'pg_trgm not available, skipping trigram indexes';
END
$$;

-- artifact.search_tsv: title (A) + body (B) so a title hit ranks above a
-- body-only hit under ts_rank.
DO $$
BEGIN
  ALTER TABLE artifact
    ADD COLUMN IF NOT EXISTS search_tsv tsvector
    GENERATED ALWAYS AS (
      setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
      setweight(to_tsvector('simple', coalesce(body, '')), 'B')
    ) STORED;
EXCEPTION WHEN duplicate_column THEN
  NULL;
END
$$;

CREATE INDEX IF NOT EXISTS idx_artifact_search_tsv
  ON artifact USING gin (search_tsv);

-- cerebro_note_comment.search_tsv: comment body only. The query joins the
-- parent artifact ON c.note_id and carries title/body rank from that row.
DO $$
BEGIN
  ALTER TABLE cerebro_note_comment
    ADD COLUMN IF NOT EXISTS search_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(body, ''))) STORED;
EXCEPTION WHEN duplicate_column THEN
  NULL;
END
$$;

CREATE INDEX IF NOT EXISTS idx_cerebro_note_comment_search_tsv
  ON cerebro_note_comment USING gin (search_tsv);

-- pg_trgm indexes for fuzzy / spelling-tolerance fallback. Wrapped so a CI
-- without pg_trgm still passes.
DO $$
BEGIN
  CREATE INDEX IF NOT EXISTS idx_artifact_title_trgm
    ON artifact USING gin (lower(title) gin_trgm_ops);
  CREATE INDEX IF NOT EXISTS idx_artifact_body_trgm
    ON artifact USING gin (lower(coalesce(body, '')) gin_trgm_ops);
  CREATE INDEX IF NOT EXISTS idx_cerebro_note_comment_body_trgm
    ON cerebro_note_comment USING gin (lower(coalesce(body, '')) gin_trgm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping trigram indexes (pg_trgm not installed)';
END
$$;
