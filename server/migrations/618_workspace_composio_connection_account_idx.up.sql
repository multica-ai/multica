-- Upsert key for a team account callback; one row per Composio account.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_composio_connection_account
    ON workspace_composio_connection (workspace_id, connected_account_id);
