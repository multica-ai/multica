-- The documents some published document supersedes; search hides them.
-- Partial: most links supersede nothing.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_link_supersedes_idx
    ON knowledge_link (workspace_id, supersedes_link_id)
    WHERE supersedes_link_id IS NOT NULL;
