-- Rollup window + triggers + cron entry for `task_usage_hourly`. Same
-- shape as 077 (per-runtime daily) and 084 (dashboard daily), the
-- differences are:
--   * bucket grain is HOUR, not DATE.
--   * bucket boundary is always UTC — regardless of any runtime tz —
--     because viewer-side tz is applied at query time.
--   * a single PK covers runtime + agent + project + provider + model,
--     so both prior pipelines collapse into this one.
--
-- IDEMPOTENCY CONTRACT (same as 073/084):
--   For every dirty key, this function REPLACES the corresponding
--   hourly row with the SUM of *all* task_usage rows for that key. It
--   does NOT delta. Re-running an overlapping window yields the same
--   final state, which is what makes "cron + offline backfill" safe to
--   run concurrently.
--
-- The window function returns the number of (upserted + deleted-empty)
-- rows. Empty buckets that recomputed to zero (because their source
-- rows were deleted or re-attributed) get removed in the same
-- transaction — same `deleted_empty` CTE pattern as 084.

-- Helper: canonical UTC hour boundary. Centralised so triggers and the
-- recompute CTE compute the same expression byte-for-byte; a drift here
-- splits the bucket key between writers and the rollup table and the
-- bucket never converges.
--
-- The two `AT TIME ZONE 'UTC'` are not redundant: the first casts
-- timestamptz -> timestamp (UTC wall clock) so date_trunc is independent
-- of the session timezone; the second casts back to timestamptz. Do not
-- collapse to a bare date_trunc — that would inherit the session tz.
CREATE OR REPLACE FUNCTION task_usage_hour_bucket(ts TIMESTAMPTZ)
RETURNS TIMESTAMPTZ
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT (date_trunc('hour', ts AT TIME ZONE 'UTC')) AT TIME ZONE 'UTC';
$$;

-- Trigger 1: agent_task_queue BEFORE UPDATE OF runtime_id / issue_id OR DELETE.
--
-- Runtime reassignment moves usage between runtime buckets; issue
-- reassignment moves it between (project_id) buckets. Both need
-- OLD and NEW keys enqueued. DELETE only enqueues OLD.
--
-- The atq trigger fires while OLD.* is still resolvable. For DELETE
-- cascades coming from `issue` DELETE, the issue row is gone by the
-- time this trigger sees the atq row, so `LEFT JOIN issue` resolves
-- project_id = NULL. The companion issue-DELETE trigger below covers
-- that case while issue is still readable.
CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_atq()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.runtime_id IS DISTINCT FROM NEW.runtime_id
           OR OLD.issue_id IS DISTINCT FROM NEW.issue_id THEN
            -- OLD side. NULL runtime_id rows are not aggregated (no
            -- runtime → no bucket); skip those.
            IF OLD.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, provider, model
                )
                SELECT DISTINCT
                    task_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    OLD.runtime_id,
                    OLD.agent_id,
                    i_old.project_id,
                    tu.provider,
                    tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = OLD.agent_id
                  LEFT JOIN issue i_old ON i_old.id = OLD.issue_id
                 WHERE tu.task_id = OLD.id
                ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;

            IF NEW.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, provider, model
                )
                SELECT DISTINCT
                    task_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    NEW.runtime_id,
                    NEW.agent_id,
                    i_new.project_id,
                    tu.provider,
                    tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = NEW.agent_id
                  LEFT JOIN issue i_new ON i_new.id = NEW.issue_id
                 WHERE tu.task_id = NEW.id
                ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.runtime_id IS NOT NULL THEN
            INSERT INTO task_usage_hourly_dirty (
                bucket_hour, workspace_id, runtime_id, agent_id,
                project_id, provider, model
            )
            SELECT DISTINCT
                task_usage_hour_bucket(tu.created_at),
                a.workspace_id,
                OLD.runtime_id,
                OLD.agent_id,
                i.project_id,
                tu.provider,
                tu.model
              FROM task_usage tu
              JOIN agent a ON a.id = OLD.agent_id
              LEFT JOIN issue i ON i.id = OLD.issue_id
             WHERE tu.task_id = OLD.id
            ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
                SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$;

-- INVARIANT: agent_task_queue.agent_id is immutable once a row is inserted.
-- If a future feature makes agent_id mutable (e.g. reassign / rebind), it
-- MUST be added to this trigger's `OF` column list, otherwise dirty
-- buckets for the old agent_id will not be enqueued and historical
-- aggregates will silently rot.
CREATE TRIGGER trg_atq_dirty_hourly
BEFORE UPDATE OF runtime_id, issue_id OR DELETE ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_hourly_dirty_for_atq();

-- Trigger 2: issue BEFORE DELETE — see the analogous trigger in 084 for
-- the full motivation. By the time the atq cascade fires, the issue
-- row is gone, so we capture project_id here while it is still readable.
CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_issue_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, provider, model
    )
    SELECT DISTINCT
        task_usage_hour_bucket(tu.created_at),
        OLD.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        OLD.project_id,
        tu.provider,
        tu.model
      FROM agent_task_queue atq
      JOIN task_usage tu ON tu.task_id = atq.id
     WHERE atq.issue_id = OLD.id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_issue_delete_dirty_hourly
BEFORE DELETE ON issue
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_hourly_dirty_for_issue_delete();

-- Trigger 3: issue BEFORE UPDATE OF project_id — re-attribute every
-- bucket touched by tasks under this issue. Same logic as 084's
-- analogous trigger.
CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_issue_project()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        -- OLD project buckets.
        INSERT INTO task_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model
        )
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            OLD.project_id,
            tu.provider,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);

        -- NEW project buckets.
        INSERT INTO task_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model
        )
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            NEW.project_id,
            tu.provider,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_issue_project_dirty_hourly
BEFORE UPDATE OF project_id ON issue
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_hourly_dirty_for_issue_project();

-- Trigger 4: task_usage BEFORE DELETE — rare in practice (no direct
-- callers today) but keeps the rollup convergent if one is added.
-- INSERT/UPDATE are covered by the watermark window.
CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_tu()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, provider, model
    )
    SELECT
        task_usage_hour_bucket(OLD.created_at),
        a.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        i.project_id,
        OLD.provider,
        OLD.model
      FROM agent_task_queue atq
      JOIN agent a ON a.id = atq.agent_id
      LEFT JOIN issue i ON i.id = atq.issue_id
     WHERE atq.id = OLD.task_id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_tu_dirty_hourly
BEFORE DELETE ON task_usage
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_hourly_dirty_for_tu();

-- Window function. Mirrors 077/084's structure:
--   1. Discover dirty keys from the updated_at watermark + the queue.
--   2. Recompute each from raw via the atq + agent + issue join.
--   3. Upsert; delete buckets that recomputed to nothing.
--   4. Drain the queue rows whose enqueued_at < p_to.
--
-- The recompute LEFT JOINs `issue`, so tasks with no issue resolve to
-- project_id = NULL and merge cleanly with the corresponding dirty key
-- via `IS NOT DISTINCT FROM`.
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
            SUM(tu.input_tokens)::bigint       AS input_tokens,
            SUM(tu.output_tokens)::bigint      AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint  AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
            COUNT(DISTINCT tu.task_id)::bigint AS task_count,
            COUNT(*)::bigint                   AS event_count
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
            task_count, event_count
        )
        SELECT
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            task_count, event_count
          FROM recomputed
        ON CONFLICT ON CONSTRAINT uq_task_usage_hourly_key DO UPDATE
            SET input_tokens       = EXCLUDED.input_tokens,
                output_tokens      = EXCLUDED.output_tokens,
                cache_read_tokens  = EXCLUDED.cache_read_tokens,
                cache_write_tokens = EXCLUDED.cache_write_tokens,
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

-- Dirty-queue TTL. The window function above already deletes drained
-- rows on every tick (`DELETE FROM task_usage_hourly_dirty WHERE
-- enqueued_at < p_to`), which bounds the queue in steady state. This
-- explicit prune is the belt-and-braces guarantee for rows that escape
-- a tick — a crash mid-tick, or the worker paused during an incident /
-- migration freeze. Without it the dirty queue grows unbounded: the
-- hourly grain multiplies the retouched-bucket surface ~24x over the
-- legacy daily rollups, and every retouched bucket leaves a row behind.
--
-- 7-day retention is generous on purpose — the only way a row survives
-- a tick is operator action (worker paused), and a week is long enough
-- that an on-call rotation will notice. Longer, and the queue size
-- starts to dominate the cost of the very draining it enables. The cron
-- entry below folds the prune in, so operators need no second job.
CREATE OR REPLACE FUNCTION prune_task_usage_hourly_dirty(
    p_retention INTERVAL DEFAULT INTERVAL '7 days'
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    DELETE FROM task_usage_hourly_dirty
     WHERE enqueued_at < now() - p_retention;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN v_rows;
END;
$$;

-- Cron entry. Uses its own advisory lock id 4246 so a tick serialises
-- only against other ticks of this same pipeline.
CREATE OR REPLACE FUNCTION rollup_task_usage_hourly()
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_lock_ok BOOLEAN;
    v_from    TIMESTAMPTZ;
    v_to      TIMESTAMPTZ;
    v_rows    BIGINT := 0;
BEGIN
    SELECT pg_try_advisory_lock(4246) INTO v_lock_ok;
    IF NOT v_lock_ok THEN
        RETURN 0;
    END IF;

    BEGIN
        UPDATE task_usage_hourly_rollup_state
           SET last_run_started_at = now(),
               last_error          = NULL
         WHERE id = 1
        RETURNING watermark_at INTO v_from;

        -- Cap each tick at a one-day window. In steady state v_from is
        -- recent, so LEAST picks `now() - 5 min` and nothing changes. But
        -- if the worker was paused (incident, migration freeze) the
        -- watermark can fall far behind; without the cap a single catch-up
        -- tick would recompute a multi-week window in one statement while
        -- holding lock 4246, blocking every other tick. Capped, catch-up
        -- advances in bounded one-day steps over successive ticks.
        v_to := LEAST(now() - INTERVAL '5 minutes', v_from + INTERVAL '1 day');

        IF v_from < v_to THEN
            v_rows := rollup_task_usage_hourly_window(v_from, v_to);

            UPDATE task_usage_hourly_rollup_state
               SET watermark_at         = v_to,
                   last_run_finished_at = now(),
                   last_run_rows        = v_rows
             WHERE id = 1;
        ELSE
            UPDATE task_usage_hourly_rollup_state
               SET last_run_finished_at = now(),
                   last_run_rows        = 0
             WHERE id = 1;
        END IF;

        PERFORM pg_advisory_unlock(4246);
    EXCEPTION WHEN OTHERS THEN
        UPDATE task_usage_hourly_rollup_state
           SET last_error           = SQLERRM,
               last_run_finished_at = now()
         WHERE id = 1;
        PERFORM pg_advisory_unlock(4246);
        RAISE;
    END;

    -- TTL prune. Runs AFTER the advisory lock is released: on a large
    -- stale backlog the prune can be slow, and holding lock 4246 through
    -- it would serialise every concurrent cron tick. It is a plain
    -- bounded DELETE — idempotent and safe to run unlocked.
    PERFORM prune_task_usage_hourly_dirty();
    RETURN v_rows;
END;
$$;

-- Health-check helper — same shape as 076 / 084 lag helpers.
CREATE OR REPLACE FUNCTION task_usage_hourly_rollup_lag_seconds()
RETURNS DOUBLE PRECISION
LANGUAGE sql
STABLE
AS $$
    SELECT EXTRACT(EPOCH FROM (now() - last_run_finished_at))
      FROM task_usage_hourly_rollup_state
     WHERE id = 1;
$$;

-- Cron registration is intentionally deferred to the operator playbook,
-- matching 076 / 084. Rollout sequence:
--   1) Apply the hourly-pipeline migrations (the task_usage_hourly schema
--      and this pipeline). The legacy daily rollups keep running untouched.
--   2) Run `go run ./cmd/backfill_task_usage_hourly` to seed history.
--   3) As superuser, schedule the cron job:
--        SELECT cron.schedule(
--          'rollup_task_usage_hourly',
--          '*/5 * * * *',
--          $$SELECT rollup_task_usage_hourly()$$
--        );
--   4) Once `task_usage_hourly` is verified against the legacy daily
--      rollups, apply the migration that drops the legacy pipelines.
