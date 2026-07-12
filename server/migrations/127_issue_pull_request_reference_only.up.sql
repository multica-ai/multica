-- Persist whether a PR ↔ issue link is justified ONLY by a bare mention of the
-- issue identifier in the PR description (body), with no closing keyword and no
-- reference in the PR title or branch name. The auto-link layer stays generous
-- and still records the link row (so close_intent can be tracked and downgraded
-- across edits), but a reference-only link is hidden from the issue's PR list:
-- a passing "Related MUL-1" / "Follow up in MUL-1" mention should not surface
-- the PR as if it were a working PR for that issue.
--
-- Defaults to FALSE so pre-existing links keep showing until their PR's next
-- webhook re-evaluates the reference.
ALTER TABLE issue_pull_request
    ADD COLUMN reference_only BOOLEAN NOT NULL DEFAULT FALSE;
