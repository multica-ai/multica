-- Re-ranking an issue into a new column reads MIN(position) for the
-- destination (workspace_id, status). idx_issue_status stops at
-- (workspace_id, status), so that min costs a heap fetch per issue in the
-- column -- paid on every status change, most often against Done, the column
-- that grows without bound. Carrying position in the index makes it an
-- index-only min. New-issue creation takes the same min and benefits equally.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_status_position
    ON issue (workspace_id, status, position);
