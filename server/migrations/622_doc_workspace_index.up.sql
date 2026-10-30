-- Tree reads and folder-emptiness checks.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_doc_workspace
    ON doc (workspace_id, folder_id);
