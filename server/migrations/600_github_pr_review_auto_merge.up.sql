-- MUL-8433: two more fields on the PR snapshot, so the card can tell "waiting
-- for approval" apart from other blocked states and show queued auto-merge.
-- Nullable with no default: rows that predate the snapshot, or a snapshot
-- whose GraphQL read failed, simply carry NULL.
ALTER TABLE github_pull_request
    -- GraphQL `reviewDecision`: APPROVED / CHANGES_REQUESTED / REVIEW_REQUIRED.
    ADD COLUMN IF NOT EXISTS review_decision TEXT,
    -- REST `auto_merge.merge_method` (merge / squash / rebase); NULL when off.
    ADD COLUMN IF NOT EXISTS auto_merge_method TEXT,
    -- REST `auto_merge.enabled_by.login`; NULL when off.
    ADD COLUMN IF NOT EXISTS auto_merge_enabled_by TEXT;
