-- Remote MCP apps (MUL-9792): apps whose own MCP server Multica connects to
-- over OAuth instead of through Composio. Multica is the OAuth client; runs
-- reach the app through the connected-apps gateway and never see a token.

-- The OAuth client this deployment registered with an app's authorization
-- server, one per (app, redirect URI). A deployment whose public URL changes
-- registers again instead of reusing a client bound to the old callback.
CREATE TABLE IF NOT EXISTS remote_app_oauth_client (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_slug                   TEXT NOT NULL,
    redirect_uri               TEXT NOT NULL,
    authorization_endpoint     TEXT NOT NULL,
    token_endpoint             TEXT NOT NULL,
    scope                      TEXT NOT NULL DEFAULT '',
    client_id                  TEXT NOT NULL,
    -- Sealed with MULTICA_APPS_SECRET_KEY; NULL for a public client.
    client_secret              BYTEA,
    token_endpoint_auth_method TEXT NOT NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One account per app and owner. owner_id is the workspace for a team
-- account and the member for a personal one, like the Composio accounts.
-- Tokens are sealed with MULTICA_APPS_SECRET_KEY and cleared on disconnect.
CREATE TABLE IF NOT EXISTS remote_app_connection (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    app_slug        TEXT NOT NULL,
    scope           TEXT NOT NULL CHECK (scope IN ('team', 'personal')),
    owner_id        UUID NOT NULL,
    connected_by    UUID NOT NULL,
    oauth_client_id UUID NOT NULL,
    access_token    BYTEA,
    refresh_token   BYTEA,
    expires_at      TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'expired', 'revoked')),
    connected_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
