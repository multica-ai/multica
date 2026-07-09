-- Re-bucket the rollup pipeline by the owning runtime's IANA timezone
-- (added in 081), instead of the Postgres session timezone (which is
-- effectively UTC in production).
--
-- This affects three places that all derive a bucket_date from
-- `task_usage.created_at`:
--
--   1. enqueue_task_usage_daily_dirty_for_atq() — trigger that enqueues
--      dirty buckets when a task_usage's atq row changes runtime or
--      gets deleted.
--   2. enqueue_task_usage_daily_dirty_for_tu()  — trigger that enqueues
--      dirty buckets when a task_usage row is deleted directly.
--   3. rollup_task_usage_daily_window()         — the cron-driven window
--      function that drains dirty keys and recomputes their buckets.
--
-- All three previously called bare `DATE(tu.created_at)`. They now call
-- `DATE(tu.created_at AT TIME ZONE rt.timezone)`, joining `agent_runtime
-- rt` along the existing path to atq.runtime_id. Skipping any one of
-- them would split the bucket key between writers (triggers / window)
-- and the rollup table itself.
--
-- *** NO HISTORICAL BACKFILL ***
-- This migration changes only the function bodies, not any data. Rows
-- already present in `task_usage_daily` keep their previously computed
-- (UTC) `bucket_date` values. From the moment this migration ships,
-- *future* writes / re-touches of any bucket recompute the date under
-- the runtime's tz. For runtimes that stay on `'UTC'` (the column
-- default) this is a no-op; for runtimes whose operators set a non-UTC
-- tz, dates only converge as their underlying raw rows get re-touched
-- by new events. The product decision (MUL-1950) was "guarantee future
-- correctness, do not backfill history".

CREATE OR REPLACE FUNCTION enqueue_task_usage_daily_dirty_for_atq()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.runtime_id IS DISTINCT FROM NEW.runtime_id THEN
            IF OLD.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
                SELECT DISTINCT
                       DATE(tu.created_at AT TIME ZONE rt.timezone),
                       a.workspace_id,
                       OLD.runtime_id,
                       tu.provider,
                       tu.model
                  FROM task_usage tu
                  JOIN agent         a  ON a.id  = OLD.agent_id
                  JOIN agent_runtime rt ON rt.id = OLD.runtime_id
                 WHERE tu.task_id = OLD.id
                ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
            IF NEW.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
                SELECT DISTINCT
                       DATE(tu.created_at AT TIME ZONE rt.timezone),
                       a.workspace_id,
                       NEW.runtime_id,
                       tu.provider,
                       tu.model
                  FROM task_usage tu
                  JOIN agent         a  ON a.id  = NEW.agent_id
                  JOIN agent_runtime rt ON rt.id = NEW.runtime_id
                 WHERE tu.task_id = NEW.id
                ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.runtime_id IS NOT NULL THEN
            INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
            SELECT DISTINCT
                   DATE(tu.created_at AT TIME ZONE rt.timezone),
                   a.workspace_id,
                   OLD.runtime_id,
                   tu.provider,
                   tu.model
              FROM task_usage tu
              JOIN agent         a  ON a.id  = OLD.agent_id
              JOIN agent_runtime rt ON rt.id = OLD.runtime_id
             WHERE tu.task_id = OLD.id
            ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
                SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_task_usage_daily_dirty_for_tu()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
    SELECT DATE(OLD.created_at AT TIME ZONE rt.timezone),
           a.workspace_id,
           atq.runtime_id,
           OLD.provider,
           OLD.model
      FROM agent_task_queue atq
      JOIN agent         a  ON a.id  = atq.agent_id
      JOIN agent_runtime rt ON rt.id = atq.runtime_id
     WHERE atq.id = OLD.task_id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
        SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE OR REPLACE FUNCTION rollup_task_usage_daily_window(
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
    -- Source 1: rows with updated_at in this window (steady state) plus
    -- the legacy-row OR branch for NULL updated_at (covered by partial
    -- index idx_task_usage_created_at_legacy from migration 078).
    --
    -- bucket_date is now derived under each runtime's IANA timezone via
    -- AT TIME ZONE rt.timezone. agent_runtime joins through atq.runtime_id
    -- which is already required NOT NULL in the same WHERE clause.
    dirty_from_updates AS (
        SELECT DISTINCT
            DATE(tu.created_at AT TIME ZONE rt.timezone) AS bucket_date,
            a.workspace_id      AS workspace_id,
            atq.runtime_id      AS runtime_id,
            tu.provider         AS provider,
            tu.model            AS model
          FROM task_usage tu
          JOIN agent_task_queue atq ON atq.id      = tu.task_id
          JOIN agent            a   ON a.id        = atq.agent_id
          JOIN agent_runtime    rt  ON rt.id       = atq.runtime_id
         WHERE atq.runtime_id IS NOT NULL
           AND (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    -- Source 2: explicit invalidation queue (deletes + reassignments).
    -- The queue rows already carry bucket_date computed under each
    -- runtime's tz at trigger time, so we don't translate again here.
    dirty_from_queue AS (
        SELECT bucket_date, workspace_id, runtime_id, provider, model
          FROM task_usage_daily_dirty
         WHERE enqueued_at < p_to
    ),
    dirty_keys AS (
        SELECT * FROM dirty_from_updates
        UNION
        SELECT * FROM dirty_from_queue
    ),
    -- Recompute each dirty bucket from ground truth. The bucket_date
    -- predicate uses the runtime's tz so it matches dirty_from_updates
    -- and the trigger functions byte-for-byte.
    recomputed AS (
        SELECT
            dk.bucket_date,
            dk.workspace_id,
            dk.runtime_id,
            dk.provider,
            dk.model,
            SUM(tu.input_tokens)::bigint        AS input_tokens,
            SUM(tu.output_tokens)::bigint       AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint   AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint  AS cache_write_tokens,
            COUNT(*)::bigint                    AS event_count
          FROM dirty_keys dk
          JOIN agent_runtime    rt  ON rt.id           = dk.runtime_id
          JOIN agent_task_queue atq ON atq.runtime_id = dk.runtime_id
          JOIN agent            a   ON a.id           = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          JOIN task_usage       tu  ON tu.task_id     = atq.id
                                    AND tu.provider   = dk.provider
                                    AND tu.model      = dk.model
                                    AND DATE(tu.created_at AT TIME ZONE rt.timezone) = dk.bucket_date
         GROUP BY 1, 2, 3, 4, 5
    ),
    -- REPLACE present buckets.
    upserted AS (
        INSERT INTO task_usage_daily AS d (
            bucket_date, workspace_id, runtime_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            event_count
        )
        SELECT
            bucket_date, workspace_id, runtime_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            event_count
          FROM recomputed
        ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
            SET input_tokens       = EXCLUDED.input_tokens,
                output_tokens      = EXCLUDED.output_tokens,
                cache_read_tokens  = EXCLUDED.cache_read_tokens,
                cache_write_tokens = EXCLUDED.cache_write_tokens,
                event_count        = EXCLUDED.event_count,
                updated_at         = now()
        RETURNING 1
    ),
    -- DELETE buckets that are dirty but have no source rows anymore.
    deleted_empty AS (
        DELETE FROM task_usage_daily d
         USING dirty_keys dk
         WHERE d.bucket_date  = dk.bucket_date
           AND d.workspace_id = dk.workspace_id
           AND d.runtime_id   = dk.runtime_id
           AND d.provider     = dk.provider
           AND d.model        = dk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_date  = dk.bucket_date
                  AND r.workspace_id = dk.workspace_id
                  AND r.runtime_id   = dk.runtime_id
                  AND r.provider     = dk.provider
                  AND r.model        = dk.model
           )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
      INTO v_rows;

    DELETE FROM task_usage_daily_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;
