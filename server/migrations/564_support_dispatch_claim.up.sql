-- An issue-wide reservation, not a run outcome. Keep rows across issue deletion;
-- workspace teardown refuses to discard an unresolved claim. A recreated
-- local host cannot silently forget a prior attempt.
-- The only key is created CONCURRENTLY in migration 565.
CREATE TABLE support_dispatch_claim (
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    issue_revision BIGINT NOT NULL CHECK (issue_revision > 0),
    staff_comment_id UUID NOT NULL,
    staff_comment_revision BIGINT NOT NULL CHECK (staff_comment_revision > 0),
    attestation_digest TEXT NOT NULL CHECK (attestation_digest ~ '^[0-9a-f]{64}$'),
    claimed_by_user_id UUID NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
