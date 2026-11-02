-- Revision history of a proposal's body (spec 2.4, plan 9.7). One row per
-- body the boss may have read: revision 1 when the proposal is filed, then one
-- per resubmission after "ask for changes". The body is the rendered proposal
-- markdown, the same text the backing issue carries.
--
-- Proposals filed before this migration have no rows here, and nothing is
-- backfilled: their earlier bodies were overwritten before history existed.
--
-- No foreign keys: the workspace and the proposal are validated in application
-- code, and the rows are removed in the same transaction as their proposal (and
-- with the workspace in teardown). The primary key is built concurrently in
-- 710 and attached in 711; the unique (proposal_id, revision) index is 712 and
-- the workspace listing index is 713.
CREATE TABLE IF NOT EXISTS proposal_revision (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    proposal_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    body TEXT NOT NULL,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('agent', 'member')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
