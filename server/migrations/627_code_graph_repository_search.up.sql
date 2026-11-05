CREATE INDEX CONCURRENTLY IF NOT EXISTS code_graph_repository_workspace_search_idx ON code_graph_repository(workspace_id,lower(repo_name),lower(branch));
