CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_provider_workspace_idx ON knowledge_provider (workspace_id, is_enabled, created_at);
