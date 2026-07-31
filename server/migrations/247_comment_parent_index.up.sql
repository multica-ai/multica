CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_parent_id
    ON comment(parent_id)
    WHERE parent_id IS NOT NULL;
