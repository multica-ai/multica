-- Restore single-comment uniqueness. This fails while any comment is still
-- bound to several runs; remove those extra receipts before rolling back.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_supplement_comment_uidx
    ON task_supplement (comment_id);
