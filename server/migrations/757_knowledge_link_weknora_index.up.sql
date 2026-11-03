-- Looks up the taxonomy of the documents a search returned, by WeKnora id.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_link_weknora_idx
    ON knowledge_link (workspace_id, weknora_knowledge_id);
