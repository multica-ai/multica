-- Serves the workspace teardown sweep of issue_channel_callback.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_channel_callback_workspace
    ON issue_channel_callback (workspace_id);
