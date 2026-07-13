-- Drop the two legacy daily rollup pipelines now that `task_usage_hourly`
-- is the only read path (see docs/timezone-architecture-rfc.md §6,
-- Phase 3). Forward-only: there is no down migration that would put the
-- data back, since by the time this ships:
--
--   * The hourly rollup has been live and writing every bucket since
--     the hourly-pipeline migration.
--   * No handler reads from `task_usage_daily` or
--     `task_usage_dashboard_daily`. The runtime PATCH path that used
--     to delete/insert into `task_usage_daily` on tz change has been
--     replaced by a single UPDATE.
--   * The `cmd/backfill_task_usage_daily` and
--     `cmd/backfill_task_usage_dashboard_daily` commands have been
--     removed. The remaining `cmd/backfill_task_usage_hourly` is the
--     only seed path going forward.
--
-- The two pg_cron entries are unscheduled below before their functions
-- are dropped, so a still-registered job cannot tick into a
-- `function does not exist` error. The cron.unschedule calls are wrapped
-- so the migration still succeeds on instances without pg_cron at all
-- (same guard pattern as migration 076).

-- ---------------------------------------------------------------------------
-- Fail-closed guard: refuse to drop the legacy daily pipelines unless the
-- hourly rollup has actually been seeded across the existing `task_usage`
-- history. `migrate up` runs the migration set straight through (no
-- per-version stop), so a self-host operator who skips the documented
-- backfill step would otherwise silently land in a state where dashboards
-- show zeros (see SELF-HOST UPGRADE ORDER in
-- cmd/backfill_task_usage_hourly/main.go and
-- docs/timezone-architecture-rfc.md §6 / §7.1). Failing loud here is the
-- only thing that turns an undetected outage into a clear migration error.
--
-- The completion signal we trust is task_usage_hourly_rollup_state.watermark_at:
--
--   * cmd/backfill_task_usage_hourly stamps it to `now() - 5 minutes` only
--     after every monthly slice succeeded (server/cmd/backfill_task_usage_hourly/main.go).
--   * rollup_task_usage_hourly() (the pg_cron entry) advances it on every
--     tick (102_task_usage_hourly_pipeline.up.sql).
--   * The default after migration 101 is `1970-01-01`, so an unrun or
--     interrupted backfill is trivially detected.
--
-- A non-empty `task_usage_hourly` is NOT a safe proxy for "backfill done":
-- the triggers in 102 only enqueue dirty keys on agent_task_queue /
-- issue / task_usage DELETE — they do not write hourly rows themselves,
-- and they fire only on writes since 102 landed. A backfill that
-- crashed mid-run, or a manual `rollup_task_usage_hourly_window` call,
-- both leave the hourly table non-empty but with a stale watermark and
-- partial history; the legacy rollups would still be the only complete
-- read path.
--
-- A fresh database (no rows in task_usage) is exempt — there is no
-- history to backfill, and rollup_task_usage_hourly() (registered as a
-- pg_cron job by the operator) will populate task_usage_hourly from the
-- first event forward via its updated_at watermark window.
-- ---------------------------------------------------------------------------

DO $$
DECLARE
    v_watermark TIMESTAMPTZ;
    v_max_event TIMESTAMPTZ;
    v_lag       INTERVAL := INTERVAL '1 hour';
BEGIN
    IF NOT EXISTS (SELECT 1 FROM task_usage LIMIT 1) THEN
        RETURN;
    END IF;

    SELECT watermark_at INTO v_watermark
      FROM task_usage_hourly_rollup_state
     WHERE id = 1;

    IF v_watermark IS NULL THEN
        RAISE EXCEPTION
            'refusing to drop legacy daily rollups: task_usage_hourly_rollup_state row is missing — apply migrations 100-102 first, then run cmd/backfill_task_usage_hourly';
    END IF;

    SELECT MAX(COALESCE(updated_at, created_at)) INTO v_max_event FROM task_usage;

    -- A successful cmd/backfill_task_usage_hourly stamps watermark_at to
    -- `now() - 5 min`; once pg_cron is registered the worker keeps it
    -- within a similar window. If the watermark trails the latest event
    -- by more than v_lag, one of these went wrong:
    --   * the backfill was never run (watermark stuck at 1970-01-01),
    --   * the backfill was interrupted before stampWatermark ran,
    --   * someone hand-seeded task_usage_hourly without stamping,
    --   * pg_cron has been off long enough to drift past the cap.
    -- In every case, dropping the legacy rollups now would remove the
    -- only read path for buckets the hourly pipeline has not proven it
    -- owns yet.
    IF v_watermark < v_max_event - v_lag THEN
        RAISE EXCEPTION
            'refusing to drop legacy daily rollups: task_usage_hourly_rollup_state.watermark_at (%) trails task_usage latest event (%) by more than % — backfill is incomplete or pg_cron is not running. Run cmd/backfill_task_usage_hourly (and let pg_cron catch up) before re-running migrate (see SELF-HOST UPGRADE ORDER in cmd/backfill_task_usage_hourly/main.go).',
            v_watermark, v_max_event, v_lag;
    END IF;
END
$$;

-- ---------------------------------------------------------------------------
-- Unschedule the legacy pg_cron jobs first (no-op when pg_cron is absent
-- or the job was never registered).
-- ---------------------------------------------------------------------------

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
        PERFORM cron.unschedule('rollup_task_usage_daily')
          FROM cron.job WHERE jobname = 'rollup_task_usage_daily';
        PERFORM cron.unschedule('rollup_task_usage_dashboard_daily')
          FROM cron.job WHERE jobname = 'rollup_task_usage_dashboard_daily';
    END IF;
END
$$;

-- ---------------------------------------------------------------------------
-- task_usage_dashboard_daily pipeline (migration 084).
-- ---------------------------------------------------------------------------

DROP TRIGGER IF EXISTS trg_issue_project_dirty_dashboard ON issue;
DROP TRIGGER IF EXISTS trg_tu_dirty_dashboard           ON task_usage;
DROP TRIGGER IF EXISTS trg_issue_delete_dirty_dashboard ON issue;
DROP TRIGGER IF EXISTS trg_atq_dirty_dashboard          ON agent_task_queue;

DROP FUNCTION IF EXISTS task_usage_dashboard_rollup_lag_seconds();
DROP FUNCTION IF EXISTS rollup_task_usage_dashboard_daily();
DROP FUNCTION IF EXISTS rollup_task_usage_dashboard_daily_window(TIMESTAMPTZ, TIMESTAMPTZ);
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_issue_project();
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_issue_delete();
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_tu();
DROP FUNCTION IF EXISTS enqueue_task_usage_dashboard_dirty_for_atq();

DROP TABLE IF EXISTS task_usage_dashboard_dirty;
DROP TABLE IF EXISTS task_usage_dashboard_rollup_state;
DROP TABLE IF EXISTS task_usage_dashboard_daily;

-- ---------------------------------------------------------------------------
-- task_usage_daily pipeline (migrations 073 / 077 / 082).
-- ---------------------------------------------------------------------------

DROP TRIGGER IF EXISTS trg_tu_dirty_rollup  ON task_usage;
DROP TRIGGER IF EXISTS trg_atq_dirty_rollup ON agent_task_queue;

DROP FUNCTION IF EXISTS task_usage_rollup_lag_seconds();
DROP FUNCTION IF EXISTS rollup_task_usage_daily();
DROP FUNCTION IF EXISTS rollup_task_usage_daily_window(TIMESTAMPTZ, TIMESTAMPTZ);
DROP FUNCTION IF EXISTS enqueue_task_usage_daily_dirty_for_tu();
DROP FUNCTION IF EXISTS enqueue_task_usage_daily_dirty_for_atq();

DROP TABLE IF EXISTS task_usage_daily_dirty;
DROP TABLE IF EXISTS task_usage_rollup_state;
DROP TABLE IF EXISTS task_usage_daily;
