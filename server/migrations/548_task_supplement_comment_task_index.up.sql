-- One comment can steer several running agents at once, so a delivery receipt
-- is identified by the (comment, run) pair instead of by the comment alone.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_supplement_comment_task_uidx
    ON task_supplement (comment_id, task_id);
