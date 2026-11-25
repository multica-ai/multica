-- Serves the member-removal cleanup, keyed by (workspace, user).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_subscriber_workspace_user
    ON project_subscriber (workspace_id, user_id);
