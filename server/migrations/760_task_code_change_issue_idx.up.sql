-- Serves issue deletion's sweep of task_code_change.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_code_change_issue
    ON task_code_change (issue_id) WHERE issue_id IS NOT NULL;
