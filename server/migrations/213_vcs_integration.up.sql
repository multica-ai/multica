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
--
-- Per the project migration rules these tables carry NO foreign keys or
-- cascades: relationships and dependent cleanup are resolved in application
-- code (DeleteVCSConnection, DeleteWorkspace, and DeleteIssue each sweep the
-- rows below in a single atomic statement). The inline UNIQUE / PRIMARY KEY
-- constraints stay — they back the ON CONFLICT upsert targets in vcs.sql.
-- Secondary indexes live in follow-up single-statement CREATE INDEX
-- CONCURRENTLY migrations (214-218), which cannot share a file with these
-- CREATE TABLEs.

CREATE TABLE IF NOT EXISTS vcs_connection (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL,
    provider                 TEXT NOT NULL DEFAULT 'forgejo'
        CHECK (provider IN ('forgejo', 'gitea', 'gitlab')),
    instance_url             TEXT NOT NULL,
    account_login            TEXT NOT NULL,
    access_token_encrypted   TEXT NOT NULL,
    webhook_secret_encrypted TEXT NOT NULL,
    connected_by_id          UUID,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, instance_url)
);

CREATE TABLE IF NOT EXISTS vcs_pull_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    connection_id     UUID NOT NULL,
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

CREATE TABLE IF NOT EXISTS issue_vcs_pull_request (
    issue_id        UUID NOT NULL,
    pull_request_id UUID NOT NULL,
    close_intent    BOOLEAN NOT NULL DEFAULT FALSE,
    -- reference_only marks a link justified ONLY by a bare body mention (no
    -- closing keyword and no title/branch reference). Mirrors the GitHub
    -- issue_pull_request column (migration 127): such links are hidden from the
    -- issue PR list and excluded from the close aggregate so a drive-by mention
    -- neither shows as a working PR nor blocks a genuine Closes sibling.
    reference_only  BOOLEAN NOT NULL DEFAULT FALSE,
    linked_by_type  TEXT,
    linked_by_id    UUID,
    linked_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, pull_request_id)
);

CREATE TABLE IF NOT EXISTS vcs_commit_status (
    connection_id UUID NOT NULL,
    sha           TEXT NOT NULL,
    context       TEXT NOT NULL,
    state         TEXT NOT NULL,
    target_url    TEXT,
    description   TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, sha, context)
);
