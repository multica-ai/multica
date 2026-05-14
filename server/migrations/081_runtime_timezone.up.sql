-- Per-runtime IANA timezone, used as the bucket boundary for daily and
-- hourly token-usage aggregation (see runtime_usage.sql + the rollup
-- function in 082_rollup_runtime_timezone.up.sql).
--
-- Defaults to 'UTC' so the migration is non-disruptive: pre-existing
-- runtimes keep their current UTC-bucketed semantics until an operator
-- (web UI) or a daemon (system zoneinfo on registration) overrides it.
-- All historical task_usage_daily rows stay UTC-cut; only buckets that
-- get re-touched by new task_usage events after this migration ships
-- get rebuilt under the runtime's tz. This is intentional — the product
-- decision was "guarantee future correctness, do not backfill history".
ALTER TABLE agent_runtime
    ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';

COMMENT ON COLUMN agent_runtime.timezone IS
    'IANA timezone (e.g. ''Asia/Shanghai''). Bucket boundary for per-day '
    'and per-hour token usage aggregation. Defaults to UTC for runtimes '
    'that existed before MUL-1950; the daemon registration / web UI '
    'overwrites this with an operator-detected value going forward.';
