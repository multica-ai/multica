-- Thread Kiro `credits` (migration 137) through the rollup.
--
-- Motivation: #4943 was reported as "task count OK, usage / amount empty" on
-- the workspace dashboard, per-issue usage, and per-runtime pages. Migration
-- 137 captured raw credits into `task_usage`, but every user-visible cost
-- surface reads from `task_usage_hourly` (dashboard, runtime trend) or from
-- SUM(task_usage.*) shapes that were still token-only (issue summary,
-- runtime by-agent, runtime by-hour). Without this migration the fix stops
-- at the raw table and the dashboard shows 0 for Kiro forever.
--
-- Scope:
--
--   1. Add `credits DOUBLE PRECISION NOT NULL DEFAULT 0` to
--      `task_usage_hourly`. Same shape / same default as the token columns,
--      so every existing row keeps working and every non-Kiro row stays at 0.
--
--   2. Update `rollup_task_usage_hourly_window` to project SUM(tu.credits)
--      into the recompute CTE and upsert it alongside the four token
--      counters. The idempotency contract is preserved: the function still
--      REPLACES the entire bucket from `task_usage`, so re-running an
--      overlapping window yields the same final state.
--
--   3. Force a targeted backfill of history so existing hourly rows pick up
--      any pre-migration credit values that were persisted by the daemon
--      after migration 137 shipped. **Only** the (bucket, workspace,
--      runtime, agent, project, provider, model) keys that actually hold
--      `task_usage.credits > 0` are enqueued into `task_usage_hourly_dirty`
--      — a global watermark rewind would take ~70 days to catch up under
--      migration 102's 1-day cap × 5-min cadence and would freeze new usage
--      during the walk. The watermark stays at its current value so live
--      Kiro turns continue rolling forward in real time.
--
-- Out of scope: `task_usage_hourly_dirty` — that queue only tracks WHICH
-- bucket to recompute, never per-column data.

ALTER TABLE task_usage_hourly
    ADD COLUMN credits DOUBLE PRECISION NOT NULL DEFAULT 0;

-- Recompute helper. Same structure as migration 102 with the addition of
-- SUM(tu.credits) in `recomputed` and the `credits` column in the upsert.
-- The rest of the pipeline (dirty queue, watermark, cron entry) is
-- unchanged and reuses this function via `rollup_task_usage_hourly`.
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
    dirty_from_updates AS (
        SELECT DISTINCT
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
                -- Legacy updated_at-NULL rows; partial index from 078.
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    dirty_from_queue AS (
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, provider, model
          FROM task_usage_hourly_dirty
         WHERE enqueued_at < p_to
    ),
    dirty_keys AS (
        SELECT * FROM dirty_from_updates
        UNION
        SELECT * FROM dirty_from_queue
    ),
    recomputed AS (
        SELECT
            dk.bucket_hour,
            dk.workspace_id,
            dk.runtime_id,
            dk.agent_id,
            dk.project_id,
            dk.provider,
            dk.model,
            SUM(tu.input_tokens)::bigint          AS input_tokens,
            SUM(tu.output_tokens)::bigint         AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint     AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint    AS cache_write_tokens,
            SUM(tu.credits)::double precision     AS credits,
            COUNT(DISTINCT tu.task_id)::bigint    AS task_count,
            COUNT(*)::bigint                      AS event_count
          FROM dirty_keys dk
          JOIN agent_task_queue atq ON atq.runtime_id  = dk.runtime_id
                                    AND atq.agent_id    = dk.agent_id
          JOIN agent            a   ON a.id            = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          LEFT JOIN issue       i   ON i.id            = atq.issue_id
          JOIN task_usage       tu  ON tu.task_id      = atq.id
                                    AND tu.provider    = dk.provider
                                    AND tu.model       = dk.model
                                    AND task_usage_hour_bucket(tu.created_at) = dk.bucket_hour
         WHERE (i.project_id IS NOT DISTINCT FROM dk.project_id)
         GROUP BY 1, 2, 3, 4, 5, 6, 7
    ),
    upserted AS (
        INSERT INTO task_usage_hourly AS d (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            credits,
            task_count, event_count
        )
        SELECT
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            credits,
            task_count, event_count
          FROM recomputed
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_key DO UPDATE
            SET input_tokens       = EXCLUDED.input_tokens,
                output_tokens      = EXCLUDED.output_tokens,
                cache_read_tokens  = EXCLUDED.cache_read_tokens,
                cache_write_tokens = EXCLUDED.cache_write_tokens,
                credits            = EXCLUDED.credits,
                task_count         = EXCLUDED.task_count,
                event_count        = EXCLUDED.event_count,
                updated_at         = now()
        RETURNING 1
    ),
    deleted_empty AS (
        DELETE FROM task_usage_hourly d
         USING dirty_keys dk
         WHERE d.bucket_hour  = dk.bucket_hour
           AND d.workspace_id = dk.workspace_id
           AND d.runtime_id   = dk.runtime_id
           AND d.agent_id     = dk.agent_id
           AND d.project_id IS NOT DISTINCT FROM dk.project_id
           AND d.provider     = dk.provider
           AND d.model        = dk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_hour  = dk.bucket_hour
                  AND r.workspace_id = dk.workspace_id
                  AND r.runtime_id   = dk.runtime_id
                  AND r.agent_id     = dk.agent_id
                  AND r.project_id IS NOT DISTINCT FROM dk.project_id
                  AND r.provider     = dk.provider
                  AND r.model        = dk.model
           )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
      INTO v_rows;

    DELETE FROM task_usage_hourly_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;

-- Targeted backfill for buckets that already carry credit rows.
--
-- Rewinding `task_usage_hourly_rollup_state.watermark_at` to epoch (an
-- earlier draft of this migration) would work in principle — the recompute
-- CTE is REPLACE-shaped — but the cron entry advances the watermark in
-- bounded one-day steps (migration 102's `LEAST(now() - 5 min, v_from + 1
-- day)`) on a 5-minute cadence (server/internal/scheduler/jobs_task_usage.go).
-- From 1970 to 2026 that's ~20k ticks ≈ 70 days of catch-up, and while the
-- watermark is walking through history the `dirty_from_updates` CTE only
-- scans stale `updated_at` windows — every NEW Kiro turn recorded during
-- catch-up would fall outside the window and its hourly bucket would stay
-- token-only. Dashboard / runtime usage would appear frozen for weeks.
--
-- Instead: enqueue only the (workspace, runtime, agent, project, provider,
-- model, bucket_hour) tuples that actually hold `task_usage.credits > 0`.
-- The dirty-queue path in `rollup_task_usage_hourly_window` recomputes
-- exactly these buckets on the next tick; the watermark stays at its
-- current value so live traffic keeps flowing into the rollup.
--
-- Scale: a workspace on Kiro CLI 2.10+ produces at most one row per
-- (task_id, provider, model), and the hour bucket collapses many tasks
-- into one dirty key, so the enqueue is bounded by the count of
-- (bucket_hour, workspace, runtime, agent, project, provider, model)
-- combinations that ever saw credit — tiny compared to a full backfill.
-- `enqueued_at` is set to `now()` so the standard `enqueued_at < p_to`
-- drain window picks these up on the next scheduled tick without any
-- operator action.
INSERT INTO task_usage_hourly_dirty (
    bucket_hour, workspace_id, runtime_id, agent_id,
    project_id, provider, model, enqueued_at
)
SELECT DISTINCT
    task_usage_hour_bucket(tu.created_at),
    a.workspace_id,
    atq.runtime_id,
    atq.agent_id,
    i.project_id,
    tu.provider,
    tu.model,
    now()
  FROM task_usage tu
  JOIN agent_task_queue atq ON atq.id      = tu.task_id
  JOIN agent            a   ON a.id        = atq.agent_id
  LEFT JOIN issue       i   ON i.id        = atq.issue_id
 WHERE tu.credits > 0
   AND atq.runtime_id IS NOT NULL
ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
    SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
