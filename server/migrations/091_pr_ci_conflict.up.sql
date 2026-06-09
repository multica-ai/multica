-- PR CI checks + merge conflict status. Adds head_sha + mergeable_state to
-- github_pull_request, plus a per-check_suite table whose rows are filtered
-- by the PR's current head_sha at query time. This lets late-arriving suites
-- for an old head SHA land in the table without polluting the current view.

ALTER TABLE github_pull_request
    ADD COLUMN head_sha TEXT NOT NULL DEFAULT '',
    ADD COLUMN mergeable_state TEXT;

CREATE TABLE github_pull_request_check_suite (
    pr_id        UUID NOT NULL REFERENCES github_pull_request(id) ON DELETE CASCADE,
    suite_id     BIGINT NOT NULL,
    head_sha     TEXT NOT NULL,
    app_id       BIGINT NOT NULL,
    conclusion   TEXT,
    status       TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (pr_id, suite_id)
);

CREATE INDEX idx_github_pr_check_suite_aggregate
    ON github_pull_request_check_suite (pr_id, head_sha, app_id, updated_at DESC);
