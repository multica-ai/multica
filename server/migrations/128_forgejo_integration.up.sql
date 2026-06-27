-- Forgejo (self-hosted Git forge) integration: per-workspace token-based
-- connections. Unlike the GitHub App model (server/migrations/079), Forgejo
-- has no app/installation concept — each workspace stores its own instance
-- URL plus an access token used for API enrichment and a webhook secret used
-- to verify inbound webhook signatures (X-Gitea-Signature, HMAC-SHA256).
--
-- Both secrets are stored as base64-encoded secretbox ciphertext (never
-- plaintext), mirroring the Slack/Feishu token-at-rest pattern. Decryption
-- uses the MULTICA_FORGEJO_SECRET_KEY box wired in cmd/server/router.go.
--
-- Mirrored Forgejo pull requests get their own tables in a later migration;
-- they are kept separate from github_pull_request because the GitHub schema is
-- coupled to App installations and check-suite app_ids that Forgejo lacks.

CREATE TABLE forgejo_connection (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    -- Base URL of the Forgejo instance, scheme + host, no trailing slash
    -- (e.g. https://forgejo.example.com). The API client appends /api/v1.
    instance_url             TEXT NOT NULL,
    -- Login (user or org) the access token authenticates as. Surfaced in the
    -- settings UI; enrichment calls run as this identity.
    account_login            TEXT NOT NULL,
    access_token_encrypted   TEXT NOT NULL,
    webhook_secret_encrypted TEXT NOT NULL,
    connected_by_id          UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- One connection per instance per workspace. Reconnecting the same
    -- instance updates the row in place (rotates token/secret).
    UNIQUE (workspace_id, instance_url)
);

CREATE INDEX idx_forgejo_connection_workspace ON forgejo_connection(workspace_id);
