CREATE INDEX CONCURRENTLY IF NOT EXISTS code_graph_build_history ON code_graph_build(repository_id,created_at DESC);
