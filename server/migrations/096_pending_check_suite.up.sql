-- Stash for check_suite webhook events that arrive before the matching PR
-- row has been mirrored (the `pull_request` and `check_suite` webhooks are
-- delivered independently and GitHub does not guarantee order). When the
-- corresponding `pull_request` event upserts the PR row, the handler drains
-- this table for that (workspace, repo, pr_number) and replays each row
-- through the normal check_suite upsert path, then deletes the pending row.
--
-- We key by (workspace_id, repo_owner, repo_name, pr_number, suite_id) so
-- repeated deliveries of the same suite while the PR is still missing are
-- idempotent — the newer payload simply overwrites the older.
CREATE TABLE github_pending_check_suite (
    workspace_id     UUID NOT NULL,
    installation_id  BIGINT NOT NULL,
    repo_owner       TEXT NOT NULL,
    repo_name        TEXT NOT NULL,
    pr_number        INTEGER NOT NULL,
    suite_id         BIGINT NOT NULL,
    head_sha         TEXT NOT NULL,
    app_id           BIGINT NOT NULL,
    conclusion       TEXT,
    status           TEXT NOT NULL,
    suite_updated_at TIMESTAMPTZ NOT NULL,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, repo_owner, repo_name, pr_number, suite_id)
);

CREATE INDEX idx_github_pending_check_suite_received_at
    ON github_pending_check_suite(received_at);
