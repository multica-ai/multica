-- One sync record per workspace and path. The sync updates it in place.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_repo_file_key_uidx
    ON knowledge_repo_file (workspace_id, path);
