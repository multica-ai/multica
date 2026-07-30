-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Both read patterns lead with issue_id and want newest-first:
--   - ListIssueDescriptionVersions renders the history panel in DESC order.
--   - GetLatestIssueDescriptionVersion resolves the open session row on every
--     description write, which is the hot path (once per autosave debounce).
-- The trailing id keeps the order total when two rows share a timestamp.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_description_version_issue
    ON issue_description_version (issue_id, created_at DESC, id DESC);
