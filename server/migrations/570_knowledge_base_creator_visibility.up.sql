CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_base_creator_visibility_idx ON knowledge_base (workspace_id, creator_id, visibility);
