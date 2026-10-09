-- Re-add the column so an instance of the release before MUL-7072 can run against
-- a rolled-back schema: that code lists reference_only in its link INSERT, and
-- the default matches what it wrote for a claimed key.
--
-- The rows the up migration deleted do not come back. They recorded only "this PR
-- body mentioned this key", which the PR body itself still says, and no read path
-- has returned them since MUL-7072.
ALTER TABLE issue_pull_request
    ADD COLUMN reference_only BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE issue_vcs_pull_request
    ADD COLUMN reference_only BOOLEAN NOT NULL DEFAULT FALSE;
