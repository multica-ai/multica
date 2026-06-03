-- GitHub PR review tracking: mirrors pull_request_review webhook events
-- so the platform can display approval status per PR and per linked issue.

CREATE TABLE github_pr_review (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pr_id           UUID NOT NULL REFERENCES github_pull_request(id) ON DELETE CASCADE,
    review_id       BIGINT NOT NULL,
    reviewer_login  TEXT NOT NULL,
    reviewer_avatar_url TEXT,
    state           TEXT NOT NULL
        CHECK (state IN ('approved', 'changes_requested', 'commented', 'dismissed', 'pending')),
    body            TEXT,
    submitted_at    TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pr_id, review_id)
);

CREATE INDEX idx_github_pr_review_pr ON github_pr_review(pr_id);
