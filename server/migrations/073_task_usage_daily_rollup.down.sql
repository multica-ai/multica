DROP FUNCTION IF EXISTS rollup_task_usage_daily();
DROP FUNCTION IF EXISTS rollup_task_usage_daily_window(TIMESTAMPTZ, TIMESTAMPTZ);
DROP TABLE IF EXISTS task_usage_rollup_state;
DROP INDEX IF EXISTS idx_task_usage_daily_workspace_date;
DROP INDEX IF EXISTS idx_task_usage_daily_runtime_date;
DROP TABLE IF EXISTS task_usage_daily;
