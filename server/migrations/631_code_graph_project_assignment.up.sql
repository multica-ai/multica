CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_project_code_graph_repository ON project_code_graph_repository (repository_id, project_id);
