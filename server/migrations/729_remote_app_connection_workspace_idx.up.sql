-- One account per owner, app and workspace: a member can hold the same app
-- in two workspaces. Replaces uq_remote_app_connection_owner (dropped in 730).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_remote_app_connection_owner_ws
    ON remote_app_connection (scope, owner_id, app_slug, workspace_id);
