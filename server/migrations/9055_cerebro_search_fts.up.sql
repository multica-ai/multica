-- FIR-2595 / search trin 1: replace LIKE-based search with Postgres FTS.
--
-- Adds a tsvector column on issue (title weighted A, description weighted B)
-- and on comment (content), each GIN-indexed, plus pg_trgm indexes for
-- spell-tolerance. Configuration "simple" keeps tokenisation language-neutral
-- (Multica issues mix Danish and English); pg_trgm trigrams cover the bøjnings-
-- og stavefejls-tolerance that ts_vector alone cannot give without a stemmer.
--
-- The tsvector columns are GENERATED ALWAYS, so any future write keeps them
-- in sync automatically — no trigger to maintain.
--
-- CI environments without these extensions skip silently so the gate stays
-- green.

DO $$
BEGIN
  CREATE EXTENSION IF NOT EXISTS pg_trgm;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'pg_trgm not available, skipping trigram indexes';
END
$$;

-- issue.search_tsv: title (A) + description (B) so a title-only hit ranks
-- above a description-only hit with ts_rank.
DO $$
BEGIN
  ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS search_tsv tsvector
    GENERATED ALWAYS AS (
      setweight(to_tsvector('simple', coalesce(title, '')), 'A') ||
      setweight(to_tsvector('simple', coalesce(description, '')), 'B')
    ) STORED;
EXCEPTION WHEN duplicate_column THEN
  -- column already present from a previous run, ignore.
  NULL;
END
$$;

CREATE INDEX IF NOT EXISTS idx_issue_search_tsv
  ON issue USING gin (search_tsv);

-- comment.search_tsv: content only. The handler joins issue ON c.issue_id and
-- carries the title/description rank from the parent issue row.
DO $$
BEGIN
  ALTER TABLE comment
    ADD COLUMN IF NOT EXISTS search_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(content, ''))) STORED;
EXCEPTION WHEN duplicate_column THEN
  NULL;
END
$$;

CREATE INDEX IF NOT EXISTS idx_comment_search_tsv
  ON comment USING gin (search_tsv);

-- pg_trgm indexes for fuzzy / spelling-tolerance fallback. Wrapped so a CI
-- without pg_trgm still passes.
DO $$
BEGIN
  CREATE INDEX IF NOT EXISTS idx_issue_title_trgm
    ON issue USING gin (lower(title) gin_trgm_ops);
  CREATE INDEX IF NOT EXISTS idx_issue_description_trgm
    ON issue USING gin (lower(coalesce(description, '')) gin_trgm_ops);
  CREATE INDEX IF NOT EXISTS idx_comment_content_trgm
    ON comment USING gin (lower(content) gin_trgm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping trigram indexes (pg_trgm not installed)';
END
$$;
