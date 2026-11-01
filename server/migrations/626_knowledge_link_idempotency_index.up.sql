-- A retried write must not duplicate a document.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_link_idempotency_uidx
    ON knowledge_link (workspace_id, idempotency_key);
