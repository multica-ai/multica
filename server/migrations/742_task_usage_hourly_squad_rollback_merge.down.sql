-- Rollback prep before 741/740 down rebuild legacy 7-column unique indexes
-- CONCURRENTLY: merge any split squad_id buckets in task_usage_hourly back into
-- a single squad_id = NULL row per 7-column key so the concurrent unique index
-- builds succeed without uniqueness violations.
--
-- Operational note: pause the rollup worker before running rollback so the
-- squad-aware rollup does not write new split squad_id rows during the window
-- between 742 down and 741/740 down finishing their CONCURRENTLY index builds
-- (if interrupted, concurrentDownIndexCleanups cleans up any INVALID index for
-- safe retry).
--
-- Materialize merged rows into a transaction-scoped temp table and DELETE
-- before INSERT as separate statements: a single CTE WITH DELETE + INSERT runs
-- under one MVCC snapshot and collides with uq_task_usage_hourly_squad_key_idx
-- when a standalone (squad_id = NULL) row already shares the 7-column key.
--
-- Unprocessed task_usage_hourly_dirty rows carrying a non-NULL squad_id are
-- discarded here so 741 down can rebuild uq_task_usage_hourly_dirty_key; those
-- buckets will recompute on the next triggering touch or watermark backfill.
CREATE TEMP TABLE _tmp_merged_task_usage_hourly ON COMMIT DROP AS
SELECT
    bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model,
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
 WHERE (bucket_hour, workspace_id, runtime_id, agent_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), provider, model) IN (
     SELECT bucket_hour, workspace_id, runtime_id, agent_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), provider, model
       FROM task_usage_hourly
      WHERE squad_id IS NOT NULL
 )
 GROUP BY 1, 2, 3, 4, 5, 6, 7;

DELETE FROM task_usage_hourly
 WHERE (bucket_hour, workspace_id, runtime_id, agent_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), provider, model) IN (
     SELECT bucket_hour, workspace_id, runtime_id, agent_id, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), provider, model
       FROM _tmp_merged_task_usage_hourly
 );

INSERT INTO task_usage_hourly (
    bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    cost_usd_ticks, uncosted_input_tokens, uncosted_output_tokens,
    uncosted_cache_read_tokens, uncosted_cache_write_tokens,
    task_count, event_count, updated_at
)
SELECT
    bucket_hour, workspace_id, runtime_id, agent_id, project_id, NULL, provider, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    cost_usd_ticks, uncosted_input_tokens, uncosted_output_tokens,
    uncosted_cache_read_tokens, uncosted_cache_write_tokens,
    task_count, event_count, updated_at
  FROM _tmp_merged_task_usage_hourly;

DELETE FROM task_usage_hourly_dirty WHERE squad_id IS NOT NULL;
