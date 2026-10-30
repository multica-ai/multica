-- Recent-updates feed across the workspace.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_doc_version_workspace_updated
    ON doc_version (workspace_id, updated_at DESC);
