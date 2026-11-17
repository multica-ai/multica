-- One folder per name in a workspace, case-insensitively, and the lookup for
-- listing a workspace's folders.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_project_folder_workspace_name
    ON project_folder (workspace_id, LOWER(name));
