-- Forgejo CI mirroring. Forgejo has no GitHub-style check_suite/app model; CI
-- surfaces as commit statuses (the "status" webhook event), one row per
-- (commit sha, context) such as "ci/woodpecker" or a Forgejo Actions job.
-- We mirror the latest state per (connection, sha, context) and aggregate onto
-- a PR by its head sha — so a status that arrives before the PR is mirrored
-- still attaches once the PR's head_sha is known (no stash table needed).

ALTER TABLE forgejo_pull_request
    ADD COLUMN head_sha TEXT NOT NULL DEFAULT '';

CREATE TABLE forgejo_commit_status (
    connection_id UUID NOT NULL REFERENCES forgejo_connection(id) ON DELETE CASCADE,
    sha           TEXT NOT NULL,
    -- context is the status check name (e.g. "ci/woodpecker"); Forgejo allows
    -- many per commit. Empty string is a valid Forgejo context, so it is part
    -- of the key rather than coalesced away.
    context       TEXT NOT NULL,
    -- Raw Forgejo state: pending | success | error | failure | warning.
    state         TEXT NOT NULL,
    target_url    TEXT,
    description   TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, sha, context)
);

CREATE INDEX idx_forgejo_commit_status_lookup
    ON forgejo_commit_status(connection_id, sha);
