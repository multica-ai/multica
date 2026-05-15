DROP TRIGGER IF EXISTS trg_tu_dirty_rollup ON task_usage;
DROP TRIGGER IF EXISTS trg_atq_dirty_rollup ON agent_task_queue;
DROP FUNCTION IF EXISTS enqueue_task_usage_daily_dirty_for_tu();
DROP FUNCTION IF EXISTS enqueue_task_usage_daily_dirty_for_atq();
DROP TABLE IF EXISTS task_usage_daily_dirty;
-- idx_task_usage_created_at_legacy is owned by 078; do not drop here.
-- The 073 down-migration recreates the older window function definition.
