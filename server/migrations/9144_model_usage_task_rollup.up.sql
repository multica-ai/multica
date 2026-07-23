-- CEREBRO-PATCH(model-usage-task-rollup): FIR-3337 gives existing consumers a
-- single read seam: canonical call events for migrated tasks and task_usage
-- only for historical tasks that have no ledger events. This avoids counting
-- the shadow writes twice while the old table remains in place.
CREATE OR REPLACE VIEW model_usage_task_rollup AS
WITH cumulative_latest AS (
    SELECT DISTINCT ON (
        task_id, provider, model, COALESCE(provider_session_id, '')
    )
        task_id,
        provider,
        model,
        input_tokens,
        output_tokens,
        reasoning_tokens,
        cache_read_tokens,
        cache_write_tokens,
        cost_cents,
        observed_at
    FROM model_usage_event
    WHERE counter_semantics = 'cumulative'
    ORDER BY
        task_id,
        provider,
        model,
        COALESCE(provider_session_id, ''),
        sequence DESC,
        observed_at DESC,
        created_at DESC
), canonical_events AS (
    SELECT
        task_id,
        provider,
        model,
        input_tokens,
        output_tokens,
        reasoning_tokens,
        cache_read_tokens,
        cache_write_tokens,
        cost_cents,
        observed_at
    FROM model_usage_event
    WHERE counter_semantics = 'delta'

    UNION ALL

    SELECT
        task_id,
        provider,
        model,
        input_tokens,
        output_tokens,
        reasoning_tokens,
        cache_read_tokens,
        cache_write_tokens,
        cost_cents,
        observed_at
    FROM cumulative_latest
), canonical AS (
    SELECT
        task_id,
        provider,
        model,
        SUM(input_tokens)::bigint AS input_tokens,
        SUM(output_tokens + reasoning_tokens)::bigint AS output_tokens,
        SUM(cache_read_tokens)::bigint AS cache_read_tokens,
        SUM(cache_write_tokens)::bigint AS cache_write_tokens,
        SUM(cost_cents)::bigint AS cost_cents,
        MIN(observed_at) AS created_at,
        MAX(observed_at) AS updated_at
    FROM canonical_events
    GROUP BY task_id, provider, model
), fallback AS (
    SELECT
        tu.task_id,
        tu.provider,
        tu.model,
        tu.input_tokens,
        tu.output_tokens,
        tu.cache_read_tokens,
        tu.cache_write_tokens,
        COALESCE(tu.cost_cents, 0)::bigint AS cost_cents,
        tu.created_at,
        COALESCE(tu.updated_at, tu.created_at) AS updated_at
    FROM task_usage tu
    WHERE tu.task_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1
          FROM model_usage_event event_presence
          WHERE event_presence.task_id = tu.task_id
            AND event_presence.provider = tu.provider
            AND event_presence.model = tu.model
      )
)
SELECT
    canonical.task_id,
    canonical.provider,
    canonical.model,
    canonical.input_tokens,
    canonical.output_tokens,
    canonical.cache_read_tokens,
    canonical.cache_write_tokens,
    canonical.cost_cents,
    canonical.created_at,
    canonical.updated_at
FROM canonical

UNION ALL

SELECT
    fallback.task_id,
    fallback.provider,
    fallback.model,
    fallback.input_tokens,
    fallback.output_tokens,
    fallback.cache_read_tokens,
    fallback.cache_write_tokens,
    fallback.cost_cents,
    fallback.created_at,
    fallback.updated_at
FROM fallback
WHERE fallback.task_id IS NOT NULL;
