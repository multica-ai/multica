-- FIR-2604 / search trin 2: semantic (vector) search alongside FTS.
--
-- Adds pgvector storage for issue + comment embeddings, an enqueue queue the
-- background worker drains, and triggers that mark content stale on write.
--
-- pgvector is OPTIONAL — if the extension is not available the migration
-- skips everything semantic and the search code degrades to keyword-only.
-- Same pattern as 9055 uses for pg_trgm so CI without the extension stays
-- green and self-host installs without superuser still boot.

-- 1. Extension. Wrapped so non-superuser / CI without the package logs and
-- continues; downstream blocks all probe for the type before using it.
DO $$
BEGIN
  CREATE EXTENSION IF NOT EXISTS vector;
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'pgvector not available, semantic search will be inactive';
END
$$;

-- 2. Embedding store. One row per (target_type, target_id). The column type
-- is built at runtime so the migration is no-op when pgvector is absent.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector') THEN
    EXECUTE $sql$
      CREATE TABLE IF NOT EXISTS cerebro_search_embedding (
        target_type   text        NOT NULL CHECK (target_type IN ('issue','comment')),
        target_id     uuid        NOT NULL,
        workspace_id  uuid        NOT NULL,
        embedding     vector(384) NOT NULL,
        model         text        NOT NULL,
        content_hash  text        NOT NULL,
        generated_at  timestamptz NOT NULL DEFAULT now(),
        PRIMARY KEY (target_type, target_id)
      )
    $sql$;

    -- workspace-scoped filter index for the search WHERE clause.
    CREATE INDEX IF NOT EXISTS idx_cerebro_search_embedding_ws
      ON cerebro_search_embedding (workspace_id, target_type);

    -- IVFFlat cosine index. lists=100 fits a small/medium workspace
    -- (~50k rows); larger installs can tune by hand later. Reindex with
    -- VACUUM ANALYZE is the standard pgvector advice once rows are loaded.
    BEGIN
      EXECUTE $sql$
        CREATE INDEX IF NOT EXISTS idx_cerebro_search_embedding_vec
          ON cerebro_search_embedding
          USING ivfflat (embedding vector_cosine_ops)
          WITH (lists = 100)
      $sql$;
    EXCEPTION WHEN OTHERS THEN
      RAISE NOTICE 'ivfflat index unavailable, falling back to sequential scan';
    END;
  ELSE
    RAISE NOTICE 'skipping cerebro_search_embedding — vector type missing';
  END IF;
END
$$;

-- 3. Generation queue. Always created, even without pgvector, so triggers
-- can enqueue and the worker is the only place that checks the extension
-- before calling out. Keeping the queue around without pgvector is harmless
-- (no worker drains it).
CREATE TABLE IF NOT EXISTS cerebro_search_embedding_queue (
  id           bigserial   PRIMARY KEY,
  target_type  text        NOT NULL CHECK (target_type IN ('issue','comment')),
  target_id    uuid        NOT NULL,
  workspace_id uuid        NOT NULL,
  enqueued_at  timestamptz NOT NULL DEFAULT now(),
  attempts     int         NOT NULL DEFAULT 0,
  last_error   text
);

-- One pending entry per target is plenty; later writes collapse onto the
-- same row so the worker re-embeds the current version.
CREATE UNIQUE INDEX IF NOT EXISTS uq_cerebro_search_embedding_queue_target
  ON cerebro_search_embedding_queue (target_type, target_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_search_embedding_queue_enqueued
  ON cerebro_search_embedding_queue (enqueued_at);

-- 4. Triggers. Any change to the content we embed (issue title/description,
-- comment content) marks the target stale. We use ON CONFLICT DO NOTHING so
-- a burst of writes collapses to one queue row.
CREATE OR REPLACE FUNCTION cerebro_search_enqueue_issue() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'INSERT'
     OR NEW.title IS DISTINCT FROM OLD.title
     OR NEW.description IS DISTINCT FROM OLD.description THEN
    INSERT INTO cerebro_search_embedding_queue (target_type, target_id, workspace_id)
    VALUES ('issue', NEW.id, NEW.workspace_id)
    ON CONFLICT (target_type, target_id) DO UPDATE
      SET enqueued_at = now(),
          attempts    = 0,
          last_error  = NULL;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cerebro_search_enqueue_issue ON issue;
CREATE TRIGGER trg_cerebro_search_enqueue_issue
  AFTER INSERT OR UPDATE OF title, description ON issue
  FOR EACH ROW EXECUTE FUNCTION cerebro_search_enqueue_issue();

CREATE OR REPLACE FUNCTION cerebro_search_enqueue_comment() RETURNS trigger AS $$
BEGIN
  IF TG_OP = 'INSERT' OR NEW.content IS DISTINCT FROM OLD.content THEN
    INSERT INTO cerebro_search_embedding_queue (target_type, target_id, workspace_id)
    VALUES ('comment', NEW.id, NEW.workspace_id)
    ON CONFLICT (target_type, target_id) DO UPDATE
      SET enqueued_at = now(),
          attempts    = 0,
          last_error  = NULL;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cerebro_search_enqueue_comment ON comment;
CREATE TRIGGER trg_cerebro_search_enqueue_comment
  AFTER INSERT OR UPDATE OF content ON comment
  FOR EACH ROW EXECUTE FUNCTION cerebro_search_enqueue_comment();

-- 5. Deletes of the source row should drop the embedding + any queue entry.
CREATE OR REPLACE FUNCTION cerebro_search_drop_issue_embedding() RETURNS trigger AS $$
BEGIN
  DELETE FROM cerebro_search_embedding_queue
    WHERE target_type = 'issue' AND target_id = OLD.id;
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector') THEN
    DELETE FROM cerebro_search_embedding
      WHERE target_type = 'issue' AND target_id = OLD.id;
  END IF;
  RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cerebro_search_drop_issue_embedding ON issue;
CREATE TRIGGER trg_cerebro_search_drop_issue_embedding
  AFTER DELETE ON issue
  FOR EACH ROW EXECUTE FUNCTION cerebro_search_drop_issue_embedding();

CREATE OR REPLACE FUNCTION cerebro_search_drop_comment_embedding() RETURNS trigger AS $$
BEGIN
  DELETE FROM cerebro_search_embedding_queue
    WHERE target_type = 'comment' AND target_id = OLD.id;
  IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'vector') THEN
    DELETE FROM cerebro_search_embedding
      WHERE target_type = 'comment' AND target_id = OLD.id;
  END IF;
  RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_cerebro_search_drop_comment_embedding ON comment;
CREATE TRIGGER trg_cerebro_search_drop_comment_embedding
  AFTER DELETE ON comment
  FOR EACH ROW EXECUTE FUNCTION cerebro_search_drop_comment_embedding();
