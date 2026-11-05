CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS code_graph_build_active ON code_graph_build(repository_id) WHERE status IN ('queued','processing');
