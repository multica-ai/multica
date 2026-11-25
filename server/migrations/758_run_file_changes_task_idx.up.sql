-- Serves task deletion cleanup and workspace teardown sweeps by task_id.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_run_file_change_task ON run_file_change (task_id);
