-- name: ListRuntimeUsage :many
-- Reads from the UTC-bucketed `task_usage_hourly` rollup table,
-- aggregated to per-(date, provider, model) under the
-- caller-supplied @tz. Powers the trend chart on the runtime detail
-- page and the per-row cost cell on the runtimes list.
--
-- @tz is required, even if the caller intends "UTC", so the bucket
-- cast is unambiguous — `bucket_hour` is UTC and the caller picks the
-- calendar boundary per request.
SELECT
    DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) AS date,
    provider,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens
FROM task_usage_hourly
WHERE runtime_id = $1
  AND bucket_hour >= sqlc.arg('since')::timestamptz
GROUP BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text), provider, model
ORDER BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) DESC, provider, model;

-- name: GetRuntimeTaskHourlyActivity :many
-- Hour-of-day distribution for queue starts. Bucketed in the viewer's
-- tz so "this runtime is busy in the afternoon" actually means
-- the operator's afternoon, not UTC's.
SELECT EXTRACT(HOUR FROM started_at AT TIME ZONE @tz::text)::int AS hour,
       COUNT(*)::int AS count
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
--
-- This view doesn't bucket by date, so it doesn't need @tz; only the
-- @since cutoff is provided in runtime-local terms (computed in Go).
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
  AND tu.created_at >= @since::timestamptz
GROUP BY atq.agent_id, tu.model
ORDER BY atq.agent_id, tu.model;

-- name: GetRuntimeUsageByHour :many
-- Per-(hour, model) token aggregates (hour ∈ 0..23) for a runtime since a
-- cutoff. Powers the "By hour" tab — shows when in the day this runtime is
-- doing real work, with model preserved for client-side cost calculation
-- (same reason as ListRuntimeUsageByAgent above). Hours with zero activity
-- are omitted; the client fills the 24-bucket axis.
--
-- Hours are extracted in the viewer's tz via @tz so afternoon
-- work bucketed at UTC 06:00 lands in 14:00 for a UTC+8 viewer.
SELECT
    EXTRACT(HOUR FROM tu.created_at AT TIME ZONE @tz::text)::int AS hour,
    tu.model,
    SUM(tu.input_tokens)::bigint AS input_tokens,
    SUM(tu.output_tokens)::bigint AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
    COUNT(DISTINCT tu.task_id)::int AS task_count
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.runtime_id = $1
  AND tu.created_at >= @since::timestamptz
GROUP BY EXTRACT(HOUR FROM tu.created_at AT TIME ZONE @tz::text), tu.model
ORDER BY hour, tu.model;
