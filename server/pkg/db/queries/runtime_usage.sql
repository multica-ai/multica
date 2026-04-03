-- name: UpsertRuntimeUsage :exec
INSERT INTO runtime_usage (id, runtime_id, date, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (runtime_id, date, provider, model)
DO UPDATE SET
    input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens,
    cache_read_tokens = EXCLUDED.cache_read_tokens,
    cache_write_tokens = EXCLUDED.cache_write_tokens,
    updated_at = datetime('now');

-- name: ListRuntimeUsage :many
SELECT * FROM runtime_usage
WHERE runtime_id = ?
ORDER BY date DESC
LIMIT ?;

-- name: GetRuntimeUsageSummary :many
SELECT provider, model,
    SUM(input_tokens) AS total_input_tokens,
    SUM(output_tokens) AS total_output_tokens,
    SUM(cache_read_tokens) AS total_cache_read_tokens,
    SUM(cache_write_tokens) AS total_cache_write_tokens
FROM runtime_usage
WHERE runtime_id = ?
GROUP BY provider, model
ORDER BY provider, model;

-- name: GetRuntimeTaskHourlyActivity :many
SELECT CAST(strftime('%H', started_at) AS INTEGER) AS hour, CAST(COUNT(*) AS INTEGER) AS count
FROM agent_task_queue
WHERE runtime_id = ? AND started_at IS NOT NULL
GROUP BY hour
ORDER BY hour;
