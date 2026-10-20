-- Unique project prefix inside a workspace. Empty prefix means "no project
-- scope", so those rows are excluded from the index.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_project_workspace_issue_prefix
    ON project (workspace_id, issue_prefix)
    WHERE issue_prefix <> '';
