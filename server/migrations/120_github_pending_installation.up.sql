-- Temporarily stores installation webhook account metadata when GitHub sends
-- installation.created before the setup callback has bound installation_id to
-- a workspace. No foreign keys: the workspace binding is resolved later by the
-- setup callback.
CREATE TABLE github_pending_installation (
    installation_id BIGINT PRIMARY KEY,
    account_login   TEXT NOT NULL CHECK (account_login <> ''),
    account_type    TEXT NOT NULL DEFAULT 'User'
        CHECK (account_type IN ('User', 'Organization')),
    account_avatar_url TEXT,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
