CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_search_index_workspace_key ON search_index_change(workspace_id,entity_type,entity_id);
