-- Gitea instance connections. Gitea has no GitHub-App "installation" concept, so
-- a connection is a workspace ↔ (instance base URL, PAT) binding. The PAT is
-- stored encrypted at rest (secretbox, same posture as Lark/Slack); no plaintext
-- token ever touches this table. One workspace may connect several instances,
-- so uniqueness is keyed on (workspace_id, base_url) — mirroring GitHub's
-- one-workspace-many-installations model.

CREATE TABLE gitea_connection (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    base_url           TEXT NOT NULL,
    token_encrypted    BYTEA NOT NULL,
    account_login      TEXT NOT NULL,
    account_avatar_url TEXT,
    connected_by_id    UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, base_url)
);

CREATE INDEX idx_gitea_connection_workspace ON gitea_connection(workspace_id);
