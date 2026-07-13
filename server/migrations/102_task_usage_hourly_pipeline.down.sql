-- Unschedule the cron job first — the operator playbook in the up
-- migration registers it, so a rollback must clear it before the
-- function it calls is dropped, or the still-registered job ticks into
-- a `function does not exist` error. Guarded so rollback still succeeds
-- where pg_cron is absent or the job was never registered (same pattern
-- as migration 103).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
        PERFORM cron.unschedule('rollup_task_usage_hourly')
          FROM cron.job WHERE jobname = 'rollup_task_usage_hourly';
    END IF;
END
$$;

DROP TRIGGER IF EXISTS trg_tu_dirty_hourly ON task_usage;
DROP TRIGGER IF EXISTS trg_issue_project_dirty_hourly ON issue;
DROP TRIGGER IF EXISTS trg_issue_delete_dirty_hourly ON issue;
DROP TRIGGER IF EXISTS trg_atq_dirty_hourly ON agent_task_queue;

DROP FUNCTION IF EXISTS task_usage_hourly_rollup_lag_seconds();
DROP FUNCTION IF EXISTS rollup_task_usage_hourly();
DROP FUNCTION IF EXISTS prune_task_usage_hourly_dirty(INTERVAL);
DROP FUNCTION IF EXISTS rollup_task_usage_hourly_window(TIMESTAMPTZ, TIMESTAMPTZ);

DROP FUNCTION IF EXISTS enqueue_task_usage_hourly_dirty_for_tu();
DROP FUNCTION IF EXISTS enqueue_task_usage_hourly_dirty_for_issue_project();
DROP FUNCTION IF EXISTS enqueue_task_usage_hourly_dirty_for_issue_delete();
DROP FUNCTION IF EXISTS enqueue_task_usage_hourly_dirty_for_atq();

DROP FUNCTION IF EXISTS task_usage_hour_bucket(TIMESTAMPTZ);
