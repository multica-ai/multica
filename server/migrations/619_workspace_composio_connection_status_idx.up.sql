-- Hot read: the workspace's active team accounts (app list, run setup, gateway).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_workspace_composio_connection_status
    ON workspace_composio_connection (workspace_id, status, toolkit_slug);
