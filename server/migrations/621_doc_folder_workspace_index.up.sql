-- Folder tree reads load a workspace's folders at once.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_doc_folder_workspace
    ON doc_folder (workspace_id, parent_id);
