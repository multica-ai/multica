-- One account per owner and app; reconnecting updates the row in place.
-- Owner first so listing an owner's accounts and deleting a workspace's
-- team accounts use it too.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_remote_app_connection_owner
    ON remote_app_connection (scope, owner_id, app_slug);
