-- The contribution dashboard reads one workspace's comments inside a date
-- window; idx_comment_workspace alone leaves the date range unindexed.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_workspace_created_at
    ON comment (workspace_id, created_at);
