-- Idempotent-upload fence: one row per (workspace, task); conflicting
-- collection uploads collide on this key.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_run_file_change_workspace_task ON run_file_change (workspace_id, task_id);
