-- 1003_issue_review_thread.up.sql
--
-- CodeRabbit review-comment mirroring. One row per GitHub PR review thread
-- (a "conversation" in GitHub UI / "comment" the user sees from CR).
--
-- Architecture:
--   - GitHub fires `pull_request_review_comment` for each inline comment;
--     we upsert one row per `gh_comment_id` (top-level inline comment id).
--   - GitHub fires `pull_request_review_thread.resolved|unresolved` when a
--     thread is resolved/unresolved; we mirror that in `state` here.
--   - The dev agent (Amelia) walks unresolved rows for her current issue,
--     posts a fix, then resolves the thread on GitHub via GraphQL — which
--     fires `pull_request_review_thread.resolved` back to us, closing the
--     loop without drift.
--
-- We deliberately key on `gh_comment_id` (BIGINT, GitHub's own primary key
-- for review_comments) rather than the GraphQL node_id. The thread node_id
-- is only present on `pull_request_review_thread` payloads; the comment
-- payload carries the numeric id reliably.

CREATE TABLE issue_review_thread (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id            UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    pr_repo             TEXT NOT NULL,
    pr_number           INTEGER NOT NULL,
    gh_comment_id       BIGINT NOT NULL,
    gh_thread_node_id   TEXT,
    file_path           TEXT NOT NULL DEFAULT '',
    line                INTEGER,
    side                TEXT,
    severity            TEXT NOT NULL DEFAULT 'unknown',
    title               TEXT NOT NULL DEFAULT '',
    body                TEXT NOT NULL DEFAULT '',
    url                 TEXT NOT NULL DEFAULT '',
    author_login        TEXT NOT NULL DEFAULT '',
    state               TEXT NOT NULL DEFAULT 'unresolved',
    resolved_by_agent   UUID,
    resolved_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT issue_review_thread_state_check
        CHECK (state IN ('unresolved', 'resolved', 'outdated', 'wont_fix')),
    CONSTRAINT issue_review_thread_severity_check
        CHECK (severity IN ('issue', 'refactor', 'nitpick', 'suggestion', 'unknown')),
    CONSTRAINT issue_review_thread_side_check
        CHECK (side IS NULL OR side IN ('LEFT', 'RIGHT')),
    CONSTRAINT issue_review_thread_gh_comment_unique UNIQUE (gh_comment_id)
);

CREATE INDEX idx_issue_review_thread_issue
    ON issue_review_thread (issue_id);

CREATE INDEX idx_issue_review_thread_issue_unresolved
    ON issue_review_thread (issue_id)
    WHERE state = 'unresolved';

CREATE INDEX idx_issue_review_thread_pr
    ON issue_review_thread (pr_repo, pr_number);
