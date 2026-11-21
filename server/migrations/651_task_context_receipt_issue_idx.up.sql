-- Serves issue deletion's sweep of task_context_receipt.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_context_receipt_issue
    ON task_context_receipt (issue_id) WHERE issue_id IS NOT NULL;
