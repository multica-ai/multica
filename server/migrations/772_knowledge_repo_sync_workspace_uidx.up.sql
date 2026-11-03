-- One sync setting per workspace.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_repo_sync_workspace_uidx
    ON knowledge_repo_sync (workspace_id);
