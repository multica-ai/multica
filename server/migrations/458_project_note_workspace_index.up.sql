-- Workspace-scoped access checks and workspace deletion cleanup filter by
-- workspace_id. Separate file from the project index because each CREATE INDEX
-- CONCURRENTLY must be the only statement in its migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_note_workspace
    ON project_note (workspace_id);
