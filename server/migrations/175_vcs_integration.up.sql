-- VCS (self-hosted Git provider: Forgejo, Gitea, GitLab) integration:
-- per-workspace token-based connections. Unlike the GitHub App model
-- (server/migrations/079), these providers have no app/installation concept —
-- each workspace stores its own instance URL plus an access token used for
-- API enrichment and a webhook secret used to verify inbound webhook signatures.
--
-- Both secrets are stored as base64-encoded secretbox ciphertext (never
-- plaintext). Decryption uses the MULTICA_VCS_SECRET_KEY box wired in
-- cmd/server/router.go.
--
-- Mirrored pull requests and commit statuses are stored in vcs_pull_request
-- and vcs_commit_status tables, separate from GitHub tables.

CREATE TABLE vcs_connection (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider                 TEXT NOT NULL DEFAULT 'forgejo'
        CHECK (provider IN ('forgejo', 'gitea', 'gitlab')),
    instance_url             TEXT NOT NULL,
    account_login            TEXT NOT NULL,
    access_token_encrypted   TEXT NOT NULL,
    webhook_secret_encrypted TEXT NOT NULL,
    connected_by_id          UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, instance_url)
);

CREATE INDEX idx_vcs_connection_workspace ON vcs_connection(workspace_id);

CREATE TABLE vcs_pull_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id     UUID NOT NULL REFERENCES vcs_connection(id) ON DELETE CASCADE,
    provider          TEXT NOT NULL DEFAULT 'forgejo'
        CHECK (provider IN ('forgejo', 'gitea', 'gitlab')),
    repo_owner        TEXT NOT NULL,
    repo_name         TEXT NOT NULL,
    pr_number         INTEGER NOT NULL,
    title             TEXT NOT NULL,
    state             TEXT NOT NULL
        CHECK (state IN ('open', 'closed', 'merged', 'draft')),
    html_url          TEXT NOT NULL,
    branch            TEXT,
    head_sha          TEXT NOT NULL DEFAULT '',
    author_login      TEXT,
    author_avatar_url TEXT,
    merged_at         TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ,
    pr_created_at     TIMESTAMPTZ NOT NULL,
    pr_updated_at     TIMESTAMPTZ NOT NULL,
    additions         INTEGER NOT NULL DEFAULT 0,
    deletions         INTEGER NOT NULL DEFAULT 0,
    changed_files     INTEGER NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connection_id, repo_owner, repo_name, pr_number)
);

CREATE INDEX idx_vcs_pull_request_workspace ON vcs_pull_request(workspace_id);
CREATE INDEX idx_vcs_pull_request_connection ON vcs_pull_request(connection_id);

CREATE TABLE issue_vcs_pull_request (
    issue_id        UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    pull_request_id UUID NOT NULL REFERENCES vcs_pull_request(id) ON DELETE CASCADE,
    close_intent    BOOLEAN NOT NULL DEFAULT FALSE,
    linked_by_type  TEXT,
    linked_by_id    UUID,
    linked_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, pull_request_id)
);

CREATE INDEX idx_issue_vcs_pull_request_pr ON issue_vcs_pull_request(pull_request_id);

CREATE TABLE vcs_commit_status (
    connection_id UUID NOT NULL REFERENCES vcs_connection(id) ON DELETE CASCADE,
    sha           TEXT NOT NULL,
    context       TEXT NOT NULL,
    state         TEXT NOT NULL,
    target_url    TEXT,
    description   TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, sha, context)
);

CREATE INDEX idx_vcs_commit_status_lookup
    ON vcs_commit_status(connection_id, sha);
