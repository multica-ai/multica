CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS code_graph_repository_identity ON code_graph_repository(workspace_id, repo_url, branch);
