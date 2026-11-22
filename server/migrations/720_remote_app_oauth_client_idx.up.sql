-- One registered client per app and callback; concurrent first connects race
-- on this and the loser reads the winner's row.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_remote_app_oauth_client_app_redirect
    ON remote_app_oauth_client (app_slug, redirect_uri);
