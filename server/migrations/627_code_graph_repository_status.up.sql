CREATE INDEX CONCURRENTLY IF NOT EXISTS code_graph_repository_workspace_status_idx ON code_graph_repository(workspace_id,status,updated_at DESC);
