-- Finds the documents saved from an issue, brief or proposal.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_knowledge_link_source
    ON knowledge_link (source_type, source_id);
