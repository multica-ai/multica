-- Add first-class OAuth 2.1 connections for hosted Remote MCP servers.
-- Authorization state is short-lived and single-use. Access/refresh tokens,
-- PKCE verifiers, and optional client secrets are always secretbox ciphertext.

ALTER TABLE plugin_installation_config
    DROP CONSTRAINT plugin_installation_config_auth_type_check;

ALTER TABLE plugin_installation_config
    ADD CONSTRAINT plugin_installation_config_auth_type_check
    CHECK (auth_type IN ('none', 'bearer', 'header', 'oauth'));

ALTER TABLE plugin_installation_config
    ADD COLUMN discovered_tools JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(discovered_tools) = 'array'),
    ADD COLUMN discovered_schema_digest TEXT
        CHECK (discovered_schema_digest IS NULL OR discovered_schema_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD CONSTRAINT plugin_installation_config_discovered_digest_check
        CHECK ((jsonb_array_length(discovered_tools) = 0) = (discovered_schema_digest IS NULL));

CREATE TABLE plugin_remote_mcp_oauth_state (
    state_hash BYTEA PRIMARY KEY CHECK (octet_length(state_hash) = 32),
    workspace_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    contribution_id UUID NOT NULL,
    actor_id UUID NOT NULL,
    endpoint TEXT NOT NULL CHECK (char_length(endpoint) BETWEEN 9 AND 2048),
    public_config JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(public_config) = 'object'),
    failure_policy TEXT NOT NULL DEFAULT 'required' CHECK (failure_policy IN ('required', 'optional')),
    authorization_endpoint TEXT NOT NULL CHECK (char_length(authorization_endpoint) BETWEEN 9 AND 2048),
    token_endpoint TEXT NOT NULL CHECK (char_length(token_endpoint) BETWEEN 9 AND 2048),
    client_id TEXT NOT NULL CHECK (char_length(client_id) BETWEEN 1 AND 2048),
    scope TEXT NOT NULL DEFAULT '' CHECK (char_length(scope) <= 4096),
    redirect_uri TEXT NOT NULL CHECK (char_length(redirect_uri) BETWEEN 9 AND 2048),
    return_to TEXT NOT NULL DEFAULT '' CHECK (char_length(return_to) <= 2048),
    secret_ciphertext BYTEA NOT NULL CHECK (octet_length(secret_ciphertext) > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);
