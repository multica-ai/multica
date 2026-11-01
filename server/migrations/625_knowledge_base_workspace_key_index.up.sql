-- One knowledge base per key in a workspace; makes provisioning idempotent.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_base_workspace_key_uidx
    ON knowledge_base (workspace_id, key);
