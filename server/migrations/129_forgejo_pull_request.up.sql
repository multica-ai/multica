-- Mirrored Forgejo pull requests and their issue links. Kept separate from
-- github_pull_request because the GitHub schema is coupled to App
-- installation_ids and check-suite app_ids that Forgejo has no equivalent for.
-- The issue-side auto-link / auto-close logic is shared at the Go layer
-- (extractIdentifiers, lookupIssueByIdentifier, advanceIssueToDone); only the
-- PR storage and link table are provider-specific.

CREATE TABLE forgejo_pull_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id     UUID NOT NULL REFERENCES forgejo_connection(id) ON DELETE CASCADE,
    repo_owner        TEXT NOT NULL,
    repo_name         TEXT NOT NULL,
    pr_number         INTEGER NOT NULL,
    title             TEXT NOT NULL,
    state             TEXT NOT NULL
        CHECK (state IN ('open', 'closed', 'merged', 'draft')),
    html_url          TEXT NOT NULL,
    branch            TEXT,
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
    -- Identity is per-connection (instance), so the same owner/name/number on
    -- two different Forgejo instances connected to one workspace stay distinct.
    UNIQUE (connection_id, repo_owner, repo_name, pr_number)
);

CREATE INDEX idx_forgejo_pull_request_workspace ON forgejo_pull_request(workspace_id);
CREATE INDEX idx_forgejo_pull_request_connection ON forgejo_pull_request(connection_id);

CREATE TABLE issue_forgejo_pull_request (
    issue_id        UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    pull_request_id UUID NOT NULL REFERENCES forgejo_pull_request(id) ON DELETE CASCADE,
    -- Mirrors issue_pull_request.close_intent: true when the PR declared an
    -- explicit closing keyword ("Closes/Fixes/Resolves MUL-X") for this issue.
    close_intent    BOOLEAN NOT NULL DEFAULT FALSE,
    linked_by_type  TEXT,
    linked_by_id    UUID,
    linked_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, pull_request_id)
);

CREATE INDEX idx_issue_forgejo_pull_request_pr ON issue_forgejo_pull_request(pull_request_id);
