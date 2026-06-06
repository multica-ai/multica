-- CEREBRO-PATCH(cerebro-comment-task): FIR-39 down migration.
DROP INDEX IF EXISTS idx_cerebro_comment_task_issue_task;
DROP TABLE IF EXISTS cerebro_comment_task;
