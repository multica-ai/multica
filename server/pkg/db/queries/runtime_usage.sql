-- name: ListRuntimeUsage :many
-- Reads from raw `task_usage`, bucketed by DATE(tu.created_at) — usage
-- report time, ~= task completion time. Since cutoff is truncated to
-- start-of-day so `days=N` yields full calendar days. This is the
-- always-correct fallback path; used when USAGE_DAILY_ROLLUP_ENABLED
-- is false (or the rollup hasn't been deployed yet).
SELECT
    DATE(tu.created_at) AS date,
    tu.provider,
    tu.model,
    SUM(tu.input_tokens)::bigint AS input_tokens,
    SUM(tu.output_tokens)::bigint AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.runtime_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz)
GROUP BY DATE(tu.created_at), tu.provider, tu.model
ORDER BY DATE(tu.created_at) DESC, tu.provider, tu.model;

-- name: ListRuntimeUsageDaily :many
-- Reads from the `task_usage_daily` rollup table maintained by
-- rollup_task_usage_daily() (scheduled every 5 min via pg_cron, or any
-- equivalent external scheduler that calls the function). Same shape as
-- ListRuntimeUsage above. Today's bucket may lag the raw table by up to
-- ~10 min (5 min cron period + 5 min rollup safety lag); intentional.
--
-- Only used when USAGE_DAILY_ROLLUP_ENABLED is true AND deploy has
-- verified that the rollup is fresh (see task_usage_rollup_lag_seconds
-- helper from migration 076).
--
-- The PK on task_usage_daily already collapses to one row per
-- (bucket_date, runtime_id, provider, model), but SUM/GROUP BY is kept
-- so future schema changes (extra dimensions promoted into the table)
-- don't silently change query semantics.
SELECT
    bucket_date AS date,
    provider,
    model,
    SUM(input_tokens)::bigint AS input_tokens,
    SUM(output_tokens)::bigint AS output_tokens,
    SUM(cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint AS cache_write_tokens
FROM task_usage_daily
WHERE runtime_id = $1
  AND bucket_date >= DATE(DATE_TRUNC('day', @since::timestamptz))
GROUP BY bucket_date, provider, model
ORDER BY bucket_date DESC, provider, model;

-- name: GetRuntimeTaskHourlyActivity :many
SELECT EXTRACT(HOUR FROM started_at)::int AS hour, COUNT(*)::int AS count
FROM agent_task_queue
WHERE runtime_id = $1 AND started_at IS NOT NULL
GROUP BY hour
ORDER BY hour;

-- name: ListRuntimeUsageByAgent :many
-- Per-(agent, model) token aggregates for a runtime since a cutoff. Powers
-- the runtime-detail "Cost by agent" tab. task_usage only carries task_id,
-- so we join the queue to expose agent_id. The model dimension is kept on
-- purpose: cost is computed client-side from a per-model pricing table, so
-- collapsing models server-side would erase the information needed to do
-- that arithmetic. The client groups by agent_id and sums cost per agent.
SELECT
    atq.agent_id,
    tu.model,
    SUM(tu.input_tokens)::bigint AS input_tokens,
    SUM(tu.output_tokens)::bigint AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.runtime_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz)
GROUP BY atq.agent_id, tu.model
ORDER BY atq.agent_id, tu.model;

-- name: GetRuntimeUsageByHour :many
-- Per-(hour, model) token aggregates (hour ∈ 0..23) for a runtime since a
-- cutoff. Powers the "By hour" tab — shows when in the day this runtime is
-- doing real work, with model preserved for client-side cost calculation
-- (same reason as ListRuntimeUsageByAgent above). Hours with zero activity
-- are omitted; the client fills the 24-bucket axis.
SELECT
    EXTRACT(HOUR FROM tu.created_at)::int AS hour,
    tu.model,
    SUM(tu.input_tokens)::bigint AS input_tokens,
    SUM(tu.output_tokens)::bigint AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.runtime_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz)
GROUP BY EXTRACT(HOUR FROM tu.created_at), tu.model
ORDER BY hour, tu.model;
