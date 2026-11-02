-- Workflow names are unique per workspace, case-insensitively, because the
-- project picker and the CLI address a workflow by name.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workflow_workspace_name
    ON issue_workflow (workspace_id, lower(name));
