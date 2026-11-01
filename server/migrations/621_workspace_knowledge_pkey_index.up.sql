-- Backing index for workspace_knowledge primary key, attached in 624.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_knowledge_pkey_uidx
    ON workspace_knowledge (workspace_id);
