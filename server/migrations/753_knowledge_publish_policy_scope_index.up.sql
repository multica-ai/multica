-- One policy per scope in a workspace; a save replaces it.
-- IF NOT EXISTS keeps a retry idempotent; an interrupted build's INVALID
-- leftover is dropped first by this migration's cleanup hook in cmd/migrate.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_publish_policy_scope_uidx
    ON knowledge_publish_policy (workspace_id, scope, scope_key);
