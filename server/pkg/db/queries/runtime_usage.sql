-- name: ListRuntimeUsage :many
-- Bucket by tu.created_at (usage report time, ~= task completion time), not
-- atq.created_at (task enqueue time), so tasks that queue one day and execute
-- the next are attributed to the day tokens were actually produced. The since
-- cutoff is truncated to start-of-day so `days=N` yields full calendar days.
--
-- Bucketing is pinned to UTC (`AT TIME ZONE 'UTC'` and DATE_TRUNC's three-arg
-- form). Without this, DATE() and DATE_TRUNC() use the DB session timezone,
-- so the same row lands in different buckets depending on the server's TZ
-- config — producing nondeterministic results across environments.
SELECT
    (tu.created_at AT TIME ZONE 'UTC')::date AS date,
    tu.provider,
    tu.model,
    SUM(tu.input_tokens)::bigint AS input_tokens,
    SUM(tu.output_tokens)::bigint AS output_tokens,
    SUM(tu.cache_read_tokens)::bigint AS cache_read_tokens,
    SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.runtime_id = $1
  AND tu.created_at >= DATE_TRUNC('day', @since::timestamptz, 'UTC')
GROUP BY (tu.created_at AT TIME ZONE 'UTC')::date, tu.provider, tu.model
ORDER BY (tu.created_at AT TIME ZONE 'UTC')::date DESC, tu.provider, tu.model;

-- name: GetRuntimeTaskHourlyActivity :many
SELECT EXTRACT(HOUR FROM started_at)::int AS hour, COUNT(*)::int AS count
FROM agent_task_queue
WHERE runtime_id = $1 AND started_at IS NOT NULL
GROUP BY hour
ORDER BY hour;
