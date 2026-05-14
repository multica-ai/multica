-- Catch joined-table changes that the `updated_at` watermark in 073 misses.
--
-- The window function in 073 finds dirty buckets via `task_usage.updated_at`.
-- That covers INSERT and UPDATE on `task_usage`, but NOT:
--   1) DELETE on `task_usage` itself (no row left to discover).
--   2) Cascade DELETE through `agent_task_queue` (issue/queue rows go away,
--      taking task_usage with them).
--   3) UPDATE of `agent_task_queue.runtime_id` — used by the runtime
--      consolidation path (`ReassignTasksToRuntime`) — which moves usage
--      from one runtime's bucket to another without touching task_usage.
--
-- Without invalidation, the rollup table diverges from raw task_usage:
-- deleted issues stay billed forever, reassigned tasks stay attributed to
-- the old runtime. The raw-table fallback path doesn't suffer from this,
-- so the two read paths would silently disagree.
--
-- Solution: an explicit `task_usage_daily_dirty` queue table populated by
-- triggers on the joined tables, drained by the rollup window function.

CREATE TABLE task_usage_daily_dirty (
    bucket_date  DATE        NOT NULL,
    workspace_id UUID        NOT NULL,
    runtime_id   UUID        NOT NULL,
    provider     TEXT        NOT NULL,
    model        TEXT        NOT NULL,
    enqueued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (bucket_date, workspace_id, runtime_id, provider, model)
);

-- Drained by enqueued_at <= cutoff in the window function. Enqueue on
-- conflict updates enqueued_at to GREATEST(existing, new) so that an
-- invalidation arriving DURING a rollup tick (between the function's
-- snapshot and its drain step) keeps an enqueued_at > p_to and
-- survives the drain. Without that, the late invalidation would be
-- silently dropped.
CREATE INDEX idx_task_usage_daily_dirty_enqueued_at
    ON task_usage_daily_dirty (enqueued_at);

-- NOTE: The partial index supporting the legacy `updated_at IS NULL`
-- branch in the rollup window function is created in migration 078 with
-- `CREATE INDEX CONCURRENTLY` to avoid blocking writes on the hot
-- task_usage table. Until 078 has been applied, the OR branch falls
-- back to a sequential scan filtered by `updated_at IS NULL`. That is
-- acceptable because the rollup function is only invoked after this
-- migration AND the backfill have run; in steady state no rows have
-- NULL updated_at.

-- Trigger function for agent_task_queue. Two cases:
--   * UPDATE of runtime_id (old != new): usage moves between runtimes.
--     Enqueue both OLD and NEW runtime buckets so both get recomputed.
--   * DELETE: row + its task_usage children are about to vanish.
--     Enqueue OLD runtime buckets so the daily rows get cleared.
-- We resolve workspace_id via `agent` (NOT via `issue`). When a DELETE
-- cascades from issue → agent_task_queue, the issue row is already gone
-- by the time this BEFORE DELETE trigger fires, so a join on `issue`
-- would return zero rows and the enqueue would silently no-op. `agent`
-- has its own ON DELETE CASCADE to atq but is not in the issue cascade
-- chain, so it's still alive.
CREATE OR REPLACE FUNCTION enqueue_task_usage_daily_dirty_for_atq()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.runtime_id IS DISTINCT FROM NEW.runtime_id THEN
            IF OLD.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
                SELECT DISTINCT DATE(tu.created_at), a.workspace_id, OLD.runtime_id, tu.provider, tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = OLD.agent_id
                 WHERE tu.task_id = OLD.id
                ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
            IF NEW.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
                SELECT DISTINCT DATE(tu.created_at), a.workspace_id, NEW.runtime_id, tu.provider, tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = NEW.agent_id
                 WHERE tu.task_id = NEW.id
                ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.runtime_id IS NOT NULL THEN
            INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
            SELECT DISTINCT DATE(tu.created_at), a.workspace_id, OLD.runtime_id, tu.provider, tu.model
              FROM task_usage tu
              JOIN agent a ON a.id = OLD.agent_id
             WHERE tu.task_id = OLD.id
            ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
                SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_atq_dirty_rollup
BEFORE UPDATE OF runtime_id OR DELETE ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_daily_dirty_for_atq();

-- Trigger function for direct task_usage DELETE (rare — direct cleanup,
-- not via cascade). UPDATE on task_usage is already covered by the
-- updated_at watermark in the window function.
-- workspace_id resolved via agent (see comment on the atq trigger
-- function for why issue is unsafe in cascade contexts).
CREATE OR REPLACE FUNCTION enqueue_task_usage_daily_dirty_for_tu()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_daily_dirty (bucket_date, workspace_id, runtime_id, provider, model)
    SELECT DATE(OLD.created_at), a.workspace_id, atq.runtime_id, OLD.provider, OLD.model
      FROM agent_task_queue atq
      JOIN agent a ON a.id = atq.agent_id
     WHERE atq.id = OLD.task_id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT (bucket_date, workspace_id, runtime_id, provider, model) DO UPDATE
        SET enqueued_at = GREATEST(task_usage_daily_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_tu_dirty_rollup
BEFORE DELETE ON task_usage
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_daily_dirty_for_tu();

-- Replace the rollup window function to also drain the dirty queue and
-- DELETE buckets that no longer have any source rows.
--
-- Pure-SQL CTE form so multiple calls in the same transaction (tests,
-- backfill scripts) don't collide on temp-table names.
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
    -- workspace_id is resolved via `agent`, NOT `issue`, to match the
    -- trigger functions above. There is no schema-level FK guaranteeing
    -- agent.workspace_id == issue.workspace_id, so mixing the two
    -- sources would let dirty_from_updates / recomputed disagree with
    -- dirty_from_queue's view of which workspace a task belongs to.
    -- Going through agent everywhere keeps trigger / discovery /
    -- recompute aligned without leaning on an unenforced invariant.
    dirty_from_updates AS (
        SELECT DISTINCT
            DATE(tu.created_at) AS bucket_date,
            a.workspace_id      AS workspace_id,
            atq.runtime_id      AS runtime_id,
            tu.provider         AS provider,
            tu.model            AS model
          FROM task_usage tu
          JOIN agent_task_queue atq ON atq.id      = tu.task_id
          JOIN agent            a   ON a.id        = atq.agent_id
         WHERE atq.runtime_id IS NOT NULL
           AND (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    -- Source 2: explicit invalidation queue (deletes + reassignments).
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
    -- Recompute each dirty bucket from ground truth. Same agent-based
    -- workspace resolution as dirty_from_updates above.
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
          JOIN agent            a   ON a.id           = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          JOIN task_usage       tu  ON tu.task_id     = atq.id
                                    AND tu.provider   = dk.provider
                                    AND tu.model      = dk.model
                                    AND DATE(tu.created_at) = dk.bucket_date
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
    -- Important: USING dirty_keys (not recomputed) so we can detect
    -- "all source rows gone" — if recomputed has no row for a key, the
    -- bucket is empty and should be removed.
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

    -- Drain the consumed dirty queue rows. Anything enqueued AFTER p_to
    -- stays for the next call — keeps the contract aligned with the
    -- watermark.
    DELETE FROM task_usage_daily_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;
