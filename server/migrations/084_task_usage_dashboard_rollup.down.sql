DROP FUNCTION IF EXISTS task_usage_dashboard_rollup_lag_seconds();
DROP FUNCTION IF EXISTS rollup_task_usage_dashboard_daily();
DROP FUNCTION IF EXISTS rollup_task_usage_dashboard_daily_window(TIMESTAMPTZ, TIMESTAMPTZ);

DROP TRIGGER IF EXISTS trg_issue_project_dirty_dashboard ON issue;
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_issue_project();

DROP TRIGGER IF EXISTS trg_tu_dirty_dashboard ON task_usage;
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_tu();

DROP TRIGGER IF EXISTS trg_issue_delete_dirty_dashboard ON issue;
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_issue_delete();

DROP TRIGGER IF EXISTS trg_atq_dirty_dashboard ON agent_task_queue;
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_atq();

DROP INDEX IF EXISTS idx_task_usage_dashboard_dirty_enqueued_at;
DROP TABLE IF EXISTS task_usage_dashboard_dirty;

DROP TABLE IF EXISTS task_usage_dashboard_rollup_state;

DROP INDEX IF EXISTS idx_task_usage_dashboard_daily_agent_date;
DROP INDEX IF EXISTS idx_task_usage_dashboard_daily_project_date;
DROP INDEX IF EXISTS idx_task_usage_dashboard_daily_workspace_date;
DROP TABLE IF EXISTS task_usage_dashboard_daily;
