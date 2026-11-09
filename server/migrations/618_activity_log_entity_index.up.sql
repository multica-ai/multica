CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_activity_log_entity_keyset
    ON activity_log (workspace_id, entity_type, entity_id, created_at DESC, id DESC)
    WHERE entity_id IS NOT NULL;
