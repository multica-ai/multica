-- Serves the workspace teardown sweep of task_code_change.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_code_change_workspace
    ON task_code_change (workspace_id);
