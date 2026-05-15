-- Daily rollup table for the workspace `/{slug}/dashboard` page. Mirrors
-- the per-runtime rollup in migration 073 (`task_usage_daily`) but indexes
-- on a different set of dimensions:
--
--   (bucket_date, workspace_id, agent_id, project_id, model)
--
-- vs. the existing 073:
--
--   (bucket_date, workspace_id, runtime_id, provider, model)
--
-- The dashboard queries don't filter by `runtime_id` and the per-runtime
-- table doesn't carry `agent_id` / `project_id`, so we materialise a
-- dedicated rollup instead of extending the existing one and balloonng
-- its cardinality (`runtime × agent × project × model` per day vs.
-- `runtime × model`). The cron entry point + dirty-queue invalidation
-- pattern is otherwise identical.
--
-- `project_id` is the project at rollup time, snapshotted via
-- `agent_task_queue.issue_id → issue.project_id`. Re-attribution after a
-- user reassigns an issue's project lands via the `issue` trigger below;
-- historical buckets that aren't touched again stay attributed where
-- they were when the rollup ran. (Operator follow-up: re-run the backfill
-- command on the affected window if a bulk project move needs to
-- propagate to old data.)
--
-- `project_id` is nullable — issues without a project still produce
-- usage rows. We use `UNIQUE NULLS NOT DISTINCT` (PG 15+) so NULL is
-- treated as a single distinct value in the unique key, which lets
-- `INSERT ... ON CONFLICT` upsert "no-project" buckets the same way it
-- handles a specific project.
--
-- Bucket date is computed in **UTC**. The per-runtime rollup uses the
-- runtime's tz (migration 082) because each row has a single runtime;
-- this rollup aggregates *across* runtimes that may sit in different
-- tzs, and there is no single correct local boundary. The dashboard
-- frontend just renders the raw ISO date.
CREATE TABLE task_usage_dashboard_daily (
    bucket_date         DATE        NOT NULL,
    workspace_id        UUID        NOT NULL,
    agent_id            UUID        NOT NULL,
    project_id          UUID,
    model               TEXT        NOT NULL,
    input_tokens        BIGINT      NOT NULL DEFAULT 0,
    output_tokens       BIGINT      NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens  BIGINT      NOT NULL DEFAULT 0,
    task_count          BIGINT      NOT NULL DEFAULT 0,
    event_count         BIGINT      NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_task_usage_dashboard_daily_key
        UNIQUE NULLS NOT DISTINCT
        (bucket_date, workspace_id, agent_id, project_id, model)
);

-- Workspace-wide reads (no project filter) hit this index.
CREATE INDEX idx_task_usage_dashboard_daily_workspace_date
    ON task_usage_dashboard_daily (workspace_id, bucket_date DESC);

-- Per-project reads. Partial index because most queries either filter to
-- one project or "all" — both extremes benefit from this layout.
CREATE INDEX idx_task_usage_dashboard_daily_project_date
    ON task_usage_dashboard_daily (workspace_id, project_id, bucket_date DESC);

-- Per-agent reads (the "by agent" panel).
CREATE INDEX idx_task_usage_dashboard_daily_agent_date
    ON task_usage_dashboard_daily (workspace_id, agent_id, bucket_date DESC);

CREATE TABLE task_usage_dashboard_rollup_state (
    id                    SMALLINT    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    watermark_at          TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01 00:00:00+00',
    last_run_started_at   TIMESTAMPTZ,
    last_run_finished_at  TIMESTAMPTZ,
    last_run_rows         BIGINT      NOT NULL DEFAULT 0,
    last_error            TEXT
);
INSERT INTO task_usage_dashboard_rollup_state (id) VALUES (1) ON CONFLICT DO NOTHING;

-- Dirty queue for invalidations the `updated_at` watermark can't see:
--   * DELETE on `task_usage` (no row left for the watermark).
--   * DELETE cascade through `agent_task_queue` (task_usage rows gone).
--   * UPDATE of `issue.project_id` — moves the bucket to a new key.
--
-- bucket key matches the rollup table so the queue can be UNIONed into
-- `dirty_keys` in the window function with no translation. project_id is
-- nullable here for the same reason as in the rollup table.
CREATE TABLE task_usage_dashboard_dirty (
    bucket_date  DATE        NOT NULL,
    workspace_id UUID        NOT NULL,
    agent_id     UUID        NOT NULL,
    project_id   UUID,
    model        TEXT        NOT NULL,
    enqueued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_task_usage_dashboard_dirty_key
        UNIQUE NULLS NOT DISTINCT
        (bucket_date, workspace_id, agent_id, project_id, model)
);

CREATE INDEX idx_task_usage_dashboard_dirty_enqueued_at
    ON task_usage_dashboard_dirty (enqueued_at);

-- Trigger 1: agent_task_queue BEFORE UPDATE OF issue_id OR DELETE.
--
-- Two cases:
--
--   * UPDATE OF issue_id — currently only `LinkTaskToIssue` (quick-create
--     tasks attaching to the issue the agent just produced) writes here,
--     moving the task from `issue_id IS NULL` to a real issue. If usage
--     already rolled up under the no-project bucket, we have to enqueue
--     both OLD (NULL project) AND NEW (the new issue's project) so the
--     next tick re-attributes the tokens.
--
--   * DELETE — direct atq deletions land here with the issue row still
--     alive, so `LEFT JOIN issue` resolves the project correctly.
--     Cascade DELETE driven from `DELETE FROM issue` is handled by
--     `enqueue_task_usage_dashboard_dirty_for_issue_delete` below (which
--     fires *before* the cascade, while `issue.project_id` is still
--     readable); this trigger may also fire during that cascade, but the
--     join returns no row → workspace_id is missing, so the JOIN on
--     `agent` keeps the enqueue safe and the resulting NULL-project key
--     no-ops on recompute (deleted_empty path).
CREATE OR REPLACE FUNCTION enqueue_task_usage_dashboard_dirty_for_atq()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.issue_id IS DISTINCT FROM NEW.issue_id THEN
            -- OLD side: project_id of OLD.issue_id (NULL when OLD.issue_id IS NULL,
            -- e.g. the quick-create starting state).
            INSERT INTO task_usage_dashboard_dirty (
                bucket_date, workspace_id, agent_id, project_id, model
            )
            SELECT DISTINCT
                DATE(tu.created_at),
                a.workspace_id,
                OLD.agent_id,
                i_old.project_id,
                tu.model
              FROM task_usage tu
              JOIN agent a ON a.id = OLD.agent_id
              LEFT JOIN issue i_old ON i_old.id = OLD.issue_id
             WHERE tu.task_id = OLD.id
            ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
                SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);

            -- NEW side: project_id of NEW.issue_id.
            INSERT INTO task_usage_dashboard_dirty (
                bucket_date, workspace_id, agent_id, project_id, model
            )
            SELECT DISTINCT
                DATE(tu.created_at),
                a.workspace_id,
                NEW.agent_id,
                i_new.project_id,
                tu.model
              FROM task_usage tu
              JOIN agent a ON a.id = NEW.agent_id
              LEFT JOIN issue i_new ON i_new.id = NEW.issue_id
             WHERE tu.task_id = NEW.id
            ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
                SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        INSERT INTO task_usage_dashboard_dirty (
            bucket_date, workspace_id, agent_id, project_id, model
        )
        SELECT DISTINCT
            DATE(tu.created_at),
            a.workspace_id,
            OLD.agent_id,
            i.project_id,
            tu.model
          FROM task_usage tu
          JOIN agent a ON a.id = OLD.agent_id
          LEFT JOIN issue i ON i.id = OLD.issue_id
         WHERE tu.task_id = OLD.id
        ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_atq_dirty_dashboard
BEFORE UPDATE OF issue_id OR DELETE ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_dashboard_dirty_for_atq();

-- Trigger 1b: issue BEFORE DELETE.
--
-- `DELETE FROM issue` cascades to `agent_task_queue` and onward to
-- `task_usage`. By the time the atq BEFORE DELETE trigger runs, the
-- issue row is gone and `LEFT JOIN issue` returns NULL for project_id,
-- so the atq trigger would enqueue a NULL-project key — the rollup row
-- under the original project would never get cleared and would keep
-- billing the workspace for tokens that no longer have a source.
--
-- This trigger fires BEFORE the cascade, while `OLD.project_id` is still
-- readable, and enqueues one dirty row per (date, agent, model) the
-- issue's tasks contributed to. The next rollup tick recomputes the
-- bucket, finds no source rows under the original project, and drops it
-- (deleted_empty path).
CREATE OR REPLACE FUNCTION enqueue_task_usage_dashboard_dirty_for_issue_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_dashboard_dirty (
        bucket_date, workspace_id, agent_id, project_id, model
    )
    SELECT DISTINCT
        DATE(tu.created_at),
        OLD.workspace_id,
        atq.agent_id,
        OLD.project_id,
        tu.model
      FROM agent_task_queue atq
      JOIN task_usage tu ON tu.task_id = atq.id
     WHERE atq.issue_id = OLD.id
    ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_issue_delete_dirty_dashboard
BEFORE DELETE ON issue
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_dashboard_dirty_for_issue_delete();

-- Trigger 2: task_usage BEFORE DELETE.
-- Rare in practice (no direct DELETE call sites today) but ensures the
-- rollup converges if one is added. UPDATE / INSERT are caught by the
-- updated_at watermark in the window function.
CREATE OR REPLACE FUNCTION enqueue_task_usage_dashboard_dirty_for_tu()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_dashboard_dirty (
        bucket_date, workspace_id, agent_id, project_id, model
    )
    SELECT
        DATE(OLD.created_at),
        a.workspace_id,
        atq.agent_id,
        i.project_id,
        OLD.model
      FROM agent_task_queue atq
      JOIN agent a ON a.id = atq.agent_id
      LEFT JOIN issue i ON i.id = atq.issue_id
     WHERE atq.id = OLD.task_id
    ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_tu_dirty_dashboard
BEFORE DELETE ON task_usage
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_dashboard_dirty_for_tu();

-- Trigger 3: issue BEFORE UPDATE OF project_id.
-- Re-attribute every (date, agent, model) bucket touched by tasks under
-- this issue: enqueue OLD project_id (so it stops claiming the tokens)
-- AND NEW project_id (so it picks them up). Both go through the same
-- dirty queue; the window function recomputes from the live join in
-- recomputed CTE, which now sees NEW.project_id, so the OLD bucket
-- either drops to 0 (deleted_empty path) or shrinks.
CREATE OR REPLACE FUNCTION enqueue_task_usage_dashboard_dirty_for_issue_project()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        -- OLD project buckets.
        INSERT INTO task_usage_dashboard_dirty (
            bucket_date, workspace_id, agent_id, project_id, model
        )
        SELECT DISTINCT
            DATE(tu.created_at),
            NEW.workspace_id,
            atq.agent_id,
            OLD.project_id,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage       tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
        ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);

        -- NEW project buckets.
        INSERT INTO task_usage_dashboard_dirty (
            bucket_date, workspace_id, agent_id, project_id, model
        )
        SELECT DISTINCT
            DATE(tu.created_at),
            NEW.workspace_id,
            atq.agent_id,
            NEW.project_id,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage       tu ON tu.task_id = atq.id
         WHERE atq.issue_id = NEW.id
        ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(task_usage_dashboard_dirty.enqueued_at, EXCLUDED.enqueued_at);
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_issue_project_dirty_dashboard
BEFORE UPDATE OF project_id ON issue
FOR EACH ROW EXECUTE FUNCTION enqueue_task_usage_dashboard_dirty_for_issue_project();

-- Window function. Same shape as 077's rollup_task_usage_daily_window:
--   1. Discover dirty keys from updated_at watermark + the explicit queue.
--   2. Recompute each from raw via the agent_task_queue + issue join.
--   3. Upsert present buckets, delete buckets that recomputed to nothing.
--   4. Drain the queue rows whose enqueued_at < p_to.
--
-- IDEMPOTENCY: re-running the same window yields the same final state,
-- because each touched key is rebuilt from raw, not deltaed.
CREATE OR REPLACE FUNCTION rollup_task_usage_dashboard_daily_window(
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
            DATE(tu.created_at) AS bucket_date,
            a.workspace_id      AS workspace_id,
            atq.agent_id        AS agent_id,
            i.project_id        AS project_id,
            tu.model            AS model
          FROM task_usage tu
          JOIN agent_task_queue atq ON atq.id      = tu.task_id
          JOIN agent            a   ON a.id        = atq.agent_id
          LEFT JOIN issue       i   ON i.id        = atq.issue_id
         WHERE (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                -- Legacy `updated_at IS NULL` rows: 077 handles this via a
                -- partial index. We rely on the same `idx_task_usage_created_at_legacy`
                -- (migration 078) for this branch.
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    dirty_from_queue AS (
        SELECT bucket_date, workspace_id, agent_id, project_id, model
          FROM task_usage_dashboard_dirty
         WHERE enqueued_at < p_to
    ),
    dirty_keys AS (
        SELECT * FROM dirty_from_updates
        UNION
        SELECT * FROM dirty_from_queue
    ),
    recomputed AS (
        SELECT
            dk.bucket_date,
            dk.workspace_id,
            dk.agent_id,
            dk.project_id,
            dk.model,
            SUM(tu.input_tokens)::bigint       AS input_tokens,
            SUM(tu.output_tokens)::bigint      AS output_tokens,
            SUM(tu.cache_read_tokens)::bigint  AS cache_read_tokens,
            SUM(tu.cache_write_tokens)::bigint AS cache_write_tokens,
            COUNT(DISTINCT tu.task_id)::bigint AS task_count,
            COUNT(*)::bigint                   AS event_count
          FROM dirty_keys dk
          JOIN agent_task_queue atq ON atq.agent_id    = dk.agent_id
          JOIN agent            a   ON a.id            = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          LEFT JOIN issue       i   ON i.id            = atq.issue_id
          JOIN task_usage       tu  ON tu.task_id      = atq.id
                                    AND tu.model       = dk.model
                                    AND DATE(tu.created_at) = dk.bucket_date
         WHERE (i.project_id IS NOT DISTINCT FROM dk.project_id)
         GROUP BY 1, 2, 3, 4, 5
    ),
    upserted AS (
        INSERT INTO task_usage_dashboard_daily AS d (
            bucket_date, workspace_id, agent_id, project_id, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            task_count, event_count
        )
        SELECT
            bucket_date, workspace_id, agent_id, project_id, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            task_count, event_count
          FROM recomputed
        ON CONFLICT ON CONSTRAINT uq_task_usage_dashboard_daily_key DO UPDATE
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
        DELETE FROM task_usage_dashboard_daily d
         USING dirty_keys dk
         WHERE d.bucket_date  = dk.bucket_date
           AND d.workspace_id = dk.workspace_id
           AND d.agent_id     = dk.agent_id
           AND d.project_id IS NOT DISTINCT FROM dk.project_id
           AND d.model        = dk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_date  = dk.bucket_date
                  AND r.workspace_id = dk.workspace_id
                  AND r.agent_id     = dk.agent_id
                  AND r.project_id IS NOT DISTINCT FROM dk.project_id
                  AND r.model        = dk.model
           )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
      INTO v_rows;

    DELETE FROM task_usage_dashboard_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;

-- Cron entry. Mirrors `rollup_task_usage_daily` (migration 073) — same
-- advisory-lock + watermark + error-recovery shape. Uses lock id 4244
-- so it serialises independently of the per-runtime rollup (4242).
CREATE OR REPLACE FUNCTION rollup_task_usage_dashboard_daily()
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_lock_ok BOOLEAN;
    v_from    TIMESTAMPTZ;
    v_to      TIMESTAMPTZ;
    v_rows    BIGINT := 0;
BEGIN
    SELECT pg_try_advisory_lock(4244) INTO v_lock_ok;
    IF NOT v_lock_ok THEN
        RETURN 0;
    END IF;

    BEGIN
        UPDATE task_usage_dashboard_rollup_state
           SET last_run_started_at = now(),
               last_error          = NULL
         WHERE id = 1
        RETURNING watermark_at INTO v_from;

        v_to := now() - INTERVAL '5 minutes';

        IF v_from < v_to THEN
            v_rows := rollup_task_usage_dashboard_daily_window(v_from, v_to);

            UPDATE task_usage_dashboard_rollup_state
               SET watermark_at         = v_to,
                   last_run_finished_at = now(),
                   last_run_rows        = v_rows
             WHERE id = 1;
        ELSE
            UPDATE task_usage_dashboard_rollup_state
               SET last_run_finished_at = now(),
                   last_run_rows        = 0
             WHERE id = 1;
        END IF;

        PERFORM pg_advisory_unlock(4244);
        RETURN v_rows;
    EXCEPTION WHEN OTHERS THEN
        UPDATE task_usage_dashboard_rollup_state
           SET last_error           = SQLERRM,
               last_run_finished_at = now()
         WHERE id = 1;
        PERFORM pg_advisory_unlock(4244);
        RAISE;
    END;
END;
$$;

-- Health-check helper mirroring task_usage_rollup_lag_seconds() (076).
-- Alert via monitoring on NULL > 15 min after deploy, or value > 900s.
CREATE OR REPLACE FUNCTION task_usage_dashboard_rollup_lag_seconds()
RETURNS DOUBLE PRECISION
LANGUAGE sql
STABLE
AS $$
    SELECT EXTRACT(EPOCH FROM (now() - last_run_finished_at))
      FROM task_usage_dashboard_rollup_state
     WHERE id = 1;
$$;

-- NOTE: cron job is NOT scheduled by this migration — same convention as
-- 076 for the per-runtime rollup. Operator playbook:
--   1) Apply migrations through 084.
--   2) Run `go run ./cmd/backfill_task_usage_dashboard_daily`.
--   3) Set USAGE_DASHBOARD_ROLLUP_ENABLED=true on the API and roll out.
--   4) As superuser:
--        SELECT cron.schedule(
--          'rollup_task_usage_dashboard_daily',
--          '*/5 * * * *',
--          $$SELECT rollup_task_usage_dashboard_daily()$$
--        );
