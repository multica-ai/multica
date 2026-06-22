-- FIR-1870: the Firtal gateway server runtime records task_usage as the SUM
-- across every tool round (recordTaskUsage), so the context-window indicator
-- over-counts the cached prefix exactly like Codex did. Store the LAST round's
-- prompt footprint here (input already includes cached for the gateway's
-- OpenAI-style accounting) so the indicator reads real occupancy. One row per
-- task; the latest run wins.

-- name: UpsertCerebroTaskContextFootprint :exec
INSERT INTO cerebro_task_context_footprint (task_id, model, input_tokens, cache_read_tokens, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (task_id) DO UPDATE SET
    model = EXCLUDED.model,
    input_tokens = EXCLUDED.input_tokens,
    cache_read_tokens = EXCLUDED.cache_read_tokens,
    updated_at = now();
