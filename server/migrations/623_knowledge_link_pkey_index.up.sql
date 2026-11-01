-- Backing index for knowledge_link primary key, attached in 624.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_link_pkey_uidx
    ON knowledge_link (id);
