-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_jira_connection_workspace
    ON jira_connection (workspace_id);
