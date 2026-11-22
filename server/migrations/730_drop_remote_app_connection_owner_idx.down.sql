-- Fails while one member holds the same app in two workspaces; remove the
-- extra accounts before rolling back.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_remote_app_connection_owner
    ON remote_app_connection (scope, owner_id, app_slug);
