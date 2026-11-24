-- Rollback prep before 762/761 down rebuild the 8-column unique indexes
-- CONCURRENTLY: merge any split autopilot_id buckets in task_usage_hourly back
-- into a single autopilot_id = NULL row per 8-column key so the concurrent
-- unique index builds succeed without uniqueness violations.
--
-- Operational note: pause the rollup worker before running rollback so the
-- autopilot-aware rollup does not write new split rows during the window
-- between 763 down and 762/761 down finishing their CONCURRENTLY index builds.
--
-- Merged rows go through a transaction-scoped temp table, deleted and then
-- inserted as separate statements, for the same reason as 742 down: one CTE
-- would collide with uq_task_usage_hourly_autopilot_key_idx when a manual
-- (autopilot_id = NULL) row already shares the 8-column key.
--
-- Unprocessed dirty rows carrying an autopilot_id are discarded so 762 down
-- can rebuild uq_task_usage_hourly_dirty_squad_key_idx; those buckets
-- recompute on the next triggering touch or watermark backfill.
CREATE TEMP TABLE _tmp_merged_task_usage_hourly_autopilot ON COMMIT DROP AS
SELECT
    bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(cost_usd_ticks)::bigint      AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))::bigint             AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))::bigint           AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens))::bigint   AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens))::bigint AS uncosted_cache_write_tokens,
    SUM(task_count)::bigint          AS task_count,
    SUM(event_count)::bigint         AS event_count,
    MAX(updated_at)                  AS updated_at
  FROM task_usage_hourly
 WHERE (bucket_hour, workspace_id, runtime_id, agent_id,
        COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(squad_id, '00000000-0000-0000-0000-000000000000'::uuid),
        provider, model) IN (
     SELECT bucket_hour, workspace_id, runtime_id, agent_id,
            COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid),
            COALESCE(squad_id, '00000000-0000-0000-0000-000000000000'::uuid),
            provider, model
       FROM task_usage_hourly
      WHERE autopilot_id IS NOT NULL
 )
 GROUP BY 1, 2, 3, 4, 5, 6, 7, 8;

DELETE FROM task_usage_hourly
 WHERE (bucket_hour, workspace_id, runtime_id, agent_id,
        COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(squad_id, '00000000-0000-0000-0000-000000000000'::uuid),
        provider, model) IN (
     SELECT bucket_hour, workspace_id, runtime_id, agent_id,
            COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid),
            COALESCE(squad_id, '00000000-0000-0000-0000-000000000000'::uuid),
            provider, model
       FROM _tmp_merged_task_usage_hourly_autopilot
 );

INSERT INTO task_usage_hourly (
    bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    cost_usd_ticks, uncosted_input_tokens, uncosted_output_tokens,
    uncosted_cache_read_tokens, uncosted_cache_write_tokens,
    task_count, event_count, updated_at
)
SELECT
    bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, NULL, provider, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    cost_usd_ticks, uncosted_input_tokens, uncosted_output_tokens,
    uncosted_cache_read_tokens, uncosted_cache_write_tokens,
    task_count, event_count, updated_at
  FROM _tmp_merged_task_usage_hourly_autopilot;

DELETE FROM task_usage_hourly_dirty WHERE autopilot_id IS NOT NULL;
