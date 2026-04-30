-- name: ListRuntimeUsage :many
-- Bucket by tu.created_at (usage report time, ~= task completion time), not
-- atq.created_at (task enqueue time), so tasks that queue one day and execute
-- the next are attributed to the day tokens were actually produced. The since
-- cutoff is truncated to start-of-day so `days=N` yields full calendar days.
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
