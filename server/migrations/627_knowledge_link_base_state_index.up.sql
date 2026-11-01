-- Lists drafts of a knowledge base without asking WeKnora.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_knowledge_link_base_state
    ON knowledge_link (knowledge_base_id, state);
