-- Composite indexes that back the cursor-paginated timeline endpoint.
-- The keyset query shape is "WHERE issue_id = $1 AND (created_at, id) < ($2, $3)
-- ORDER BY created_at DESC, id DESC LIMIT $4". With (issue_id, created_at DESC,
-- id DESC) the planner can serve this as an index-only scan with no sort.
-- The leading issue_id column also covers the simple "WHERE issue_id = $1"
-- lookups, making the previous single-column idx_comment_issue and
-- idx_activity_log_issue redundant.
--
-- Not using CREATE INDEX CONCURRENTLY because the migration runner wraps
-- multi-statement files in an implicit transaction, which conflicts with
-- CONCURRENTLY. For pre-production scale this brief lock is acceptable; if
-- this ever needs to run on a hot prod table, do it as a one-off ops step.

CREATE INDEX IF NOT EXISTS idx_comment_issue_keyset
    ON comment (issue_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_activity_log_issue_keyset
    ON activity_log (issue_id, created_at DESC, id DESC);

DROP INDEX IF EXISTS idx_comment_issue;
DROP INDEX IF EXISTS idx_activity_log_issue;
