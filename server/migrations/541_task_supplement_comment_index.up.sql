CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_supplement_comment_uidx
    ON task_supplement (comment_id);
