-- Recompute the hourly usage rollup per hour key (the pre-735 7-column key,
-- without squad_id) instead of per touched squad key.
--
-- Before 735 one task_usage_hourly row held the standalone and every squad's
-- usage for an hour. 739 recomputed only the squad keys an update touched
-- (plus squad_id = NULL), so touching one run rewrote that legacy row to the
-- touched side's share and dropped the untouched runs' usage from the rollup.
--
-- Every dirty key is now widened to its hour key; all squad and standalone
-- buckets under that hour key are recomputed from raw task_usage, and rows
-- under it that no longer have usage are deleted. A legacy row is therefore
-- replaced as a whole the first time its hour is recomputed. Nothing is
-- backfilled here: legacy hours keep their combined totals until their next
-- recompute.

CREATE OR REPLACE FUNCTION rollup_task_usage_hourly_window(
    p_from TIMESTAMPTZ,
    p_to   TIMESTAMPTZ
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    IF p_from >= p_to THEN
        RETURN 0;
    END IF;

    WITH
    dirty_hour_keys AS (
        SELECT
            task_usage_hour_bucket(tu.created_at) AS bucket_hour,
            a.workspace_id                        AS workspace_id,
            atq.runtime_id                        AS runtime_id,
            atq.agent_id                          AS agent_id,
            i.project_id                          AS project_id,
            tu.provider                           AS provider,
            tu.model                              AS model
          FROM task_usage tu
          JOIN agent_task_queue atq ON atq.id      = tu.task_id
          JOIN agent            a   ON a.id        = atq.agent_id
          LEFT JOIN issue       i   ON i.id        = atq.issue_id
         WHERE atq.runtime_id IS NOT NULL
           AND (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
        UNION
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, provider, model
          FROM task_usage_hourly_dirty
         WHERE enqueued_at < p_to
    ),
    recomputed AS (
        SELECT
            hk.bucket_hour,
            hk.workspace_id,
            hk.runtime_id,
            hk.agent_id,
            hk.project_id,
            COALESCE(atq.usage_squad_id, atq.squad_id) AS squad_id,
            hk.provider,
            hk.model,
            SUM(tu.input_tokens)::bigint       AS input_tokens,
            SUM(tu.output_tokens)::bigint      AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint  AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
            COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS cost_usd_ticks,
            COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
            COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
            COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
            COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
            COUNT(DISTINCT tu.task_id)::bigint AS task_count,
            COUNT(*)::bigint                   AS event_count
          FROM dirty_hour_keys hk
          JOIN agent_task_queue atq ON atq.runtime_id  = hk.runtime_id
                                    AND atq.agent_id    = hk.agent_id
          JOIN agent            a   ON a.id            = atq.agent_id
                                    AND a.workspace_id = hk.workspace_id
          LEFT JOIN issue       i   ON i.id            = atq.issue_id
          JOIN task_usage       tu  ON tu.task_id      = atq.id
                                    AND tu.provider    = hk.provider
                                    AND tu.model       = hk.model
                                    AND task_usage_hour_bucket(tu.created_at) = hk.bucket_hour
         WHERE (i.project_id IS NOT DISTINCT FROM hk.project_id)
         GROUP BY 1, 2, 3, 4, 5, 6, 7, 8
    ),
    upserted AS (
        INSERT INTO task_usage_hourly AS d (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, squad_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
        )
        SELECT
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, squad_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
          FROM recomputed
        ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model) DO UPDATE
            SET input_tokens                = EXCLUDED.input_tokens,
                output_tokens               = EXCLUDED.output_tokens,
                cache_read_tokens           = EXCLUDED.cache_read_tokens,
                cache_write_tokens          = EXCLUDED.cache_write_tokens,
                cost_usd_ticks              = EXCLUDED.cost_usd_ticks,
                uncosted_input_tokens       = EXCLUDED.uncosted_input_tokens,
                uncosted_output_tokens      = EXCLUDED.uncosted_output_tokens,
                uncosted_cache_read_tokens  = EXCLUDED.uncosted_cache_read_tokens,
                uncosted_cache_write_tokens = EXCLUDED.uncosted_cache_write_tokens,
                task_count                  = EXCLUDED.task_count,
                event_count                 = EXCLUDED.event_count,
                updated_at                  = now()
        RETURNING 1
    ),
    deleted_empty AS (
        DELETE FROM task_usage_hourly d
         USING dirty_hour_keys hk
         WHERE d.bucket_hour  = hk.bucket_hour
           AND d.workspace_id = hk.workspace_id
           AND d.runtime_id   = hk.runtime_id
           AND d.agent_id     = hk.agent_id
           AND d.project_id IS NOT DISTINCT FROM hk.project_id
           AND d.provider     = hk.provider
           AND d.model        = hk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_hour  = d.bucket_hour
                  AND r.workspace_id = d.workspace_id
                  AND r.runtime_id   = d.runtime_id
                  AND r.agent_id     = d.agent_id
                  AND r.project_id IS NOT DISTINCT FROM d.project_id
                  AND r.squad_id   IS NOT DISTINCT FROM d.squad_id
                  AND r.provider     = d.provider
                  AND r.model        = d.model
           )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
      INTO v_rows;

    DELETE FROM task_usage_hourly_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;
