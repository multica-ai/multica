-- Serves the workspace teardown sweep of task_context_receipt.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_context_receipt_workspace
    ON task_context_receipt (workspace_id);
