CREATE INDEX CONCURRENTLY IF NOT EXISTS code_graph_build_queue ON code_graph_build(created_at) WHERE status='queued';
