-- Daily rollup table for `task_usage`. Background: the dashboard query
-- ListRuntimeUsage runs `SUM() GROUP BY DATE(created_at), provider, model`
-- against the raw event stream and is called once per runtime row on the
-- runtimes list (plus once per detail page load), so it dominates DB load
-- as event volume grows. We materialise the day-bucketed aggregate here
-- so reads scan O(days × providers × models) rows instead of O(events).
--
-- All query dimensions are denormalised into the table so reads never
-- need to join `agent_task_queue`. The PK doubles as the upsert key for
-- the rollup worker.
CREATE TABLE task_usage_daily (
    bucket_date         DATE        NOT NULL,
    workspace_id        UUID        NOT NULL,
    runtime_id          UUID        NOT NULL,
    provider            TEXT        NOT NULL,
    model               TEXT        NOT NULL,
    input_tokens        BIGINT      NOT NULL DEFAULT 0,
    output_tokens       BIGINT      NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens  BIGINT      NOT NULL DEFAULT 0,
    event_count         BIGINT      NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (bucket_date, workspace_id, runtime_id, provider, model)
);

-- Primary read path: runtime detail page + runtimes-list cost cell, both
-- filter by runtime_id and order by date DESC. bucket_date DESC in the
-- index lets the query avoid an extra sort.
CREATE INDEX idx_task_usage_daily_runtime_date
    ON task_usage_daily (runtime_id, bucket_date DESC);

-- Workspace-wide aggregations hit this index instead of fanning out per
-- runtime.
CREATE INDEX idx_task_usage_daily_workspace_date
    ON task_usage_daily (workspace_id, bucket_date DESC);

-- Single-row state table tracking how far the rollup worker has consumed.
CREATE TABLE task_usage_rollup_state (
    id                    SMALLINT    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    watermark_at          TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01 00:00:00+00',
    last_run_started_at   TIMESTAMPTZ,
    last_run_finished_at  TIMESTAMPTZ,
    last_run_rows         BIGINT      NOT NULL DEFAULT 0,
    last_error            TEXT
);
INSERT INTO task_usage_rollup_state (id) VALUES (1) ON CONFLICT DO NOTHING;

-- Window-based aggregation primitive. Used by both the cron-driven
-- watermark advancer and the offline backfill command, so they stay
-- byte-identical in their semantics. Returns the number of output rows
-- touched.
--
-- IDEMPOTENCY CONTRACT (this is the important bit):
--   For every (bucket_date, workspace_id, runtime_id, provider, model)
--   key that has at least one task_usage row whose `updated_at` falls in
--   [p_from, p_to), this function REPLACES the corresponding daily row
--   with the SUM of *all* task_usage rows for that key (regardless of
--   their updated_at). It does NOT add a delta.
--
-- Consequences:
--   * Replaying the same window is safe — the row is rebuilt from raw
--     each time, so the result converges.
--   * Two callers (cron + backfill) processing overlapping windows is
--     safe — both write the same value.
--   * `UpsertTaskUsage` corrections that overwrite token counts are
--     captured: the corrected row's updated_at gets bumped, the next
--     window picks up its bucket key, and the bucket is recomputed
--     from current truth.
--
-- Cost: the recompute reads ALL task_usage rows for each dirty bucket,
-- not just the windowed slice. In steady state only "today" buckets are
-- dirty (a handful of keys per active runtime), so this stays cheap.
-- During backfill the entire history's bucket keys become dirty once;
-- the backfill walks history in monthly slices to bound the working
-- set per call.
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

    WITH dirty_keys AS (
        SELECT DISTINCT
            DATE(tu.created_at) AS bucket_date,
            i.workspace_id      AS workspace_id,
            atq.runtime_id      AS runtime_id,
            tu.provider         AS provider,
            tu.model            AS model
        FROM task_usage tu
        JOIN agent_task_queue atq ON atq.id      = tu.task_id
        JOIN issue            i   ON i.id        = atq.issue_id
        WHERE atq.runtime_id IS NOT NULL
          AND (
              -- Steady state: rows updated within the watermark window.
              -- Hits idx_task_usage_updated_at directly.
              (tu.updated_at >= p_from AND tu.updated_at < p_to)
              -- Legacy rows from before migration 072 (updated_at IS NULL)
              -- — discoverable via created_at + the partial index added
              -- in 077. Steady-state windows after backfill never include
              -- historical dates, so this branch is a no-op once the
              -- backfill has swept history.
              OR (tu.updated_at IS NULL
                  AND tu.created_at >= p_from
                  AND tu.created_at <  p_to)
          )
    ),
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
        JOIN agent_task_queue atq ON atq.runtime_id = dk.runtime_id
        JOIN issue            i   ON i.id           = atq.issue_id
                                  AND i.workspace_id = dk.workspace_id
        JOIN task_usage       tu  ON tu.task_id     = atq.id
                                  AND tu.provider   = dk.provider
                                  AND tu.model      = dk.model
                                  AND DATE(tu.created_at) = dk.bucket_date
        GROUP BY 1, 2, 3, 4, 5
    )
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
            updated_at         = now();

    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN v_rows;
END;
$$;

-- Cron entry point. Advances the watermark by one window each call.
--
-- Invariants:
--  * `pg_try_advisory_lock(4242)` serialises overlapping ticks.
--  * The window upper bound is `now() - 5 minutes`. The lag exists
--    because `task_usage` rows are written from a separate transaction;
--    a row with updated_at = T can become visible to this snapshot at
--    some t > T. 5 minutes is a generous bound on that visibility delay
--    and keeps the dashboard "today" bucket at most ~10 min stale
--    (5 min lag + 5 min cron period).
--  * On error we record `last_error` and re-raise; the watermark is NOT
--    advanced because the UPDATE that advances it only runs after the
--    upsert succeeds.
--  * SAFE TO RUN CONCURRENTLY WITH BACKFILL: the window primitive is
--    idempotent (see contract above), so even if cron fires while the
--    offline backfill is also walking history, the worst case is some
--    bucket gets written twice with the same value.
CREATE OR REPLACE FUNCTION rollup_task_usage_daily()
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_lock_ok BOOLEAN;
    v_from    TIMESTAMPTZ;
    v_to      TIMESTAMPTZ;
    v_rows    BIGINT := 0;
BEGIN
    SELECT pg_try_advisory_lock(4242) INTO v_lock_ok;
    IF NOT v_lock_ok THEN
        RETURN 0;
    END IF;

    BEGIN
        UPDATE task_usage_rollup_state
           SET last_run_started_at = now(),
               last_error          = NULL
         WHERE id = 1
        RETURNING watermark_at INTO v_from;

        v_to := now() - INTERVAL '5 minutes';

        IF v_from < v_to THEN
            v_rows := rollup_task_usage_daily_window(v_from, v_to);

            UPDATE task_usage_rollup_state
               SET watermark_at         = v_to,
                   last_run_finished_at = now(),
                   last_run_rows        = v_rows
             WHERE id = 1;
        ELSE
            UPDATE task_usage_rollup_state
               SET last_run_finished_at = now(),
                   last_run_rows        = 0
             WHERE id = 1;
        END IF;

        PERFORM pg_advisory_unlock(4242);
        RETURN v_rows;
    EXCEPTION WHEN OTHERS THEN
        UPDATE task_usage_rollup_state
           SET last_error           = SQLERRM,
               last_run_finished_at = now()
         WHERE id = 1;
        PERFORM pg_advisory_unlock(4242);
        RAISE;
    END;
END;
$$;
