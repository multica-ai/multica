-- One kind per key in a workspace; the seed and the admin edit both rely on it.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_kind_workspace_key_uidx
    ON knowledge_kind (workspace_id, key);
