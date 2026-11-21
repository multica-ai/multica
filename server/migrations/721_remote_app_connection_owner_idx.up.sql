-- One account per app and owner; reconnecting updates the row in place.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_remote_app_connection_owner
    ON remote_app_connection (app_slug, scope, owner_id);
