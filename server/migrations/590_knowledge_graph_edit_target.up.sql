CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_graph_edit_target_idx ON knowledge_graph_edit (knowledge_base_id, target_id, created_at);
