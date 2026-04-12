-- name: UpsertRuntimeUsage :exec
INSERT INTO runtime_usage (runtime_id, date, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (runtime_id, date, provider, model)
DO UPDATE SET
    input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens,
    cache_read_tokens = EXCLUDED.cache_read_tokens,
    cache_write_tokens = EXCLUDED.cache_write_tokens,
    updated_at = now();

-- name: ListRuntimeUsage :many
SELECT * FROM runtime_usage
WHERE runtime_id = $1
  AND date >= $2
ORDER BY date DESC;

-- name: GetRuntimeUsageSummary :many
SELECT provider, model,
    SUM(input_tokens)::bigint AS total_input_tokens,
    SUM(output_tokens)::bigint AS total_output_tokens,
    SUM(cache_read_tokens)::bigint AS total_cache_read_tokens,
    SUM(cache_write_tokens)::bigint AS total_cache_write_tokens
FROM runtime_usage
WHERE runtime_id = $1
GROUP BY provider, model
ORDER BY provider, model;

-- name: GetRuntimeTaskHourlyActivity :many
SELECT EXTRACT(HOUR FROM started_at)::int AS hour, COUNT(*)::int AS count
FROM agent_task_queue
WHERE runtime_id = $1 AND started_at IS NOT NULL
GROUP BY hour
ORDER BY hour;
