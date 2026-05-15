-- Enable pg_cron extension if available, but DO NOT schedule the rollup
-- job here. Scheduling must happen *after* a successful backfill run, so
-- the cron tick doesn't race the backfill (both write the same daily
-- buckets — the rollup function in 073 is now idempotent, so collisions
-- produce correct values, but we still avoid overlap as a defense in
-- depth + to keep load low during backfill).
--
-- Operator playbook (in deployment runbook):
--   1) Apply migrations 072..075 (this file is 076).
--   2) Run `go run ./cmd/backfill_task_usage_daily` — succeeds and
--      stamps the rollup-state watermark.
--   3) Set USAGE_DAILY_ROLLUP_ENABLED=true on the API and roll out.
--   4) As superuser:
--        SELECT cron.schedule(
--          'rollup_task_usage_daily',
--          '*/5 * * * *',
--          $$SELECT rollup_task_usage_daily()$$
--        );
--   5) As superuser, also schedule cron-log pruning (see notes below).
--
-- The CREATE EXTENSION is wrapped in DO/EXCEPTION so dev/CI environments
-- without `shared_preload_libraries=pg_cron` skip gracefully and the
-- migration still succeeds (mirrors migration 032 pg_bigm pattern).
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_cron;
EXCEPTION
    WHEN OTHERS THEN
        RAISE NOTICE 'pg_cron extension not available; skipping. Schedule rollup_task_usage_daily() via your platform''s scheduling primitive (Kubernetes CronJob, etc.).';
END
$$;

-- Health check helper. Returns NULL if the rollup has never run, or the
-- number of seconds since the last successful tick. Use this from
-- monitoring / alerts:
--   * Alert if NULL for >15 minutes after deployment (cron not scheduled).
--   * Alert if value > 900 seconds (cron stuck or job failing).
CREATE OR REPLACE FUNCTION task_usage_rollup_lag_seconds()
RETURNS DOUBLE PRECISION
LANGUAGE sql
STABLE
AS $$
    SELECT EXTRACT(EPOCH FROM (now() - last_run_finished_at))
      FROM task_usage_rollup_state
     WHERE id = 1;
$$;
