-- Rollup window + triggers + cron entry for `knowledge_effect_hourly`.
-- Same shape as 102 (task_usage_hourly_pipeline), the differences are:
--   * No runtime_id dimension — knowledge effect is about tasks, not runtimes.
--   * task_kind is computed from FK columns (chat_session_id, autopilot_run_id, etc.).
--   * has_injection dimension determined by EXISTS on knowledge_injection_event.
--   * Tracks task outcomes (success/failure), duration, reruns, follow-ups.
--   * Duration only counts tasks with both started_at AND completed_at.
--
-- IDEMPOTENCY CONTRACT (same as 102):
--   For every dirty key, this function REPLACES the corresponding hourly row
--   with the SUM of *all* source rows for that key. It does NOT delta.
--   Re-running an overlapping window yields the same final state.

-- Helper: canonical UTC hour boundary. Same expression as task_usage_hour_bucket.
CREATE OR REPLACE FUNCTION knowledge_effect_hour_bucket(ts TIMESTAMPTZ)
RETURNS TIMESTAMPTZ
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT (date_trunc('hour', ts AT TIME ZONE 'UTC')) AT TIME ZONE 'UTC';
$$;

-- Helper: compute task kind from FK columns. Centralised so triggers and
-- the recompute CTE use the same logic byte-for-byte.
CREATE OR REPLACE FUNCTION compute_task_kind(
    chat_session_id    UUID,
    autopilot_run_id   UUID,
    issue_id           UUID,
    trigger_comment_id UUID
) RETURNS TEXT
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN chat_session_id IS NOT NULL THEN 'chat'
        WHEN autopilot_run_id IS NOT NULL THEN 'autopilot'
        WHEN issue_id IS NULL THEN 'quick_create'
        WHEN trigger_comment_id IS NOT NULL THEN 'comment'
        ELSE 'direct'
    END;
$$;

-- Trigger 1: knowledge_injection_event AFTER INSERT OR DELETE.
-- When a task gains or loses injection events, both has_injection=true
-- and has_injection=false buckets must be invalidated so the old value
-- zeroes out and the new value appears.
CREATE OR REPLACE FUNCTION enqueue_knowledge_effect_dirty_for_injection()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    v_task_id UUID;
BEGIN
    v_task_id := COALESCE(NEW.agent_task_id, OLD.agent_task_id);
    IF v_task_id IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    INSERT INTO knowledge_effect_hourly_dirty (
        bucket_hour, workspace_id, agent_id, project_id,
        model, provider, task_kind, has_injection
    )
    SELECT DISTINCT
        knowledge_effect_hour_bucket(tu.created_at),
        a.workspace_id,
        atq.agent_id,
        i.project_id,
        tu.model,
        tu.provider,
        compute_task_kind(atq.chat_session_id, atq.autopilot_run_id, atq.issue_id, atq.trigger_comment_id),
        inj.has_inj
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    JOIN task_usage tu ON tu.task_id = atq.id
    CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
    WHERE atq.id = v_task_id
    ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);

    RETURN COALESCE(NEW, OLD);
END;
$$;

CREATE TRIGGER trg_knowledge_injection_dirty_effect
AFTER INSERT OR DELETE ON knowledge_injection_event
FOR EACH ROW EXECUTE FUNCTION enqueue_knowledge_effect_dirty_for_injection();

-- Trigger 2: agent_task_queue AFTER UPDATE OF (status, started_at, completed_at, issue_id) OR DELETE.
-- Status/time changes affect task counts and duration metrics.
-- Issue reassignment moves the task between project buckets.
-- DELETE removes the task from all buckets.
--
-- INVARIANT: agent_task_queue.agent_id is immutable once inserted. If that
-- ever changes, agent_id MUST be added to this trigger's OF column list.
CREATE OR REPLACE FUNCTION enqueue_knowledge_effect_dirty_for_atq()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.status IS DISTINCT FROM NEW.status
           OR OLD.started_at IS DISTINCT FROM NEW.started_at
           OR OLD.completed_at IS DISTINCT FROM NEW.completed_at
           OR OLD.issue_id IS DISTINCT FROM NEW.issue_id THEN

            -- Enqueue for current task state with both has_injection values.
            -- When issue_id changed, the recompute JOIN will resolve the
            -- correct project_id from the NEW issue.
            INSERT INTO knowledge_effect_hourly_dirty (
                bucket_hour, workspace_id, agent_id, project_id,
                model, provider, task_kind, has_injection
            )
            SELECT DISTINCT
                knowledge_effect_hour_bucket(tu.created_at),
                a.workspace_id,
                NEW.agent_id,
                i_new.project_id,
                tu.model,
                tu.provider,
                compute_task_kind(NEW.chat_session_id, NEW.autopilot_run_id, NEW.issue_id, NEW.trigger_comment_id),
                inj.has_inj
            FROM task_usage tu
            JOIN agent a ON a.id = NEW.agent_id
            LEFT JOIN issue i_new ON i_new.id = NEW.issue_id
            CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
            WHERE tu.task_id = NEW.id
            ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
                SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);

            -- If issue_id changed, also enqueue OLD project buckets.
            IF OLD.issue_id IS DISTINCT FROM NEW.issue_id AND OLD.issue_id IS NOT NULL THEN
                INSERT INTO knowledge_effect_hourly_dirty (
                    bucket_hour, workspace_id, agent_id, project_id,
                    model, provider, task_kind, has_injection
                )
                SELECT DISTINCT
                    knowledge_effect_hour_bucket(tu.created_at),
                    a.workspace_id,
                    NEW.agent_id,
                    i_old.project_id,
                    tu.model,
                    tu.provider,
                    compute_task_kind(NEW.chat_session_id, NEW.autopilot_run_id, NEW.issue_id, NEW.trigger_comment_id),
                    inj.has_inj
                FROM task_usage tu
                JOIN agent a ON a.id = NEW.agent_id
                LEFT JOIN issue i_old ON i_old.id = OLD.issue_id
                CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
                WHERE tu.task_id = NEW.id
                ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
                    SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        INSERT INTO knowledge_effect_hourly_dirty (
            bucket_hour, workspace_id, agent_id, project_id,
            model, provider, task_kind, has_injection
        )
        SELECT DISTINCT
            knowledge_effect_hour_bucket(tu.created_at),
            a.workspace_id,
            OLD.agent_id,
            i.project_id,
            tu.model,
            tu.provider,
            compute_task_kind(OLD.chat_session_id, OLD.autopilot_run_id, OLD.issue_id, OLD.trigger_comment_id),
            inj.has_inj
        FROM task_usage tu
        JOIN agent a ON a.id = OLD.agent_id
        LEFT JOIN issue i ON i.id = OLD.issue_id
        CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
        WHERE tu.task_id = OLD.id
        ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_atq_dirty_knowledge_effect
AFTER UPDATE OF status, started_at, completed_at, issue_id OR DELETE ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION enqueue_knowledge_effect_dirty_for_atq();

-- Trigger 3: issue BEFORE DELETE.
-- The atq cascade fires after the issue row is gone, so we capture
-- project_id here while it is still readable.
CREATE OR REPLACE FUNCTION enqueue_knowledge_effect_dirty_for_issue_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO knowledge_effect_hourly_dirty (
        bucket_hour, workspace_id, agent_id, project_id,
        model, provider, task_kind, has_injection
    )
    SELECT DISTINCT
        knowledge_effect_hour_bucket(tu.created_at),
        OLD.workspace_id,
        atq.agent_id,
        OLD.project_id,
        tu.model,
        tu.provider,
        compute_task_kind(atq.chat_session_id, atq.autopilot_run_id, atq.issue_id, atq.trigger_comment_id),
        inj.has_inj
    FROM agent_task_queue atq
    JOIN task_usage tu ON tu.task_id = atq.id
    CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
    WHERE atq.issue_id = OLD.id
      AND atq.status IN ('completed', 'failed')
    ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_issue_delete_dirty_knowledge_effect
BEFORE DELETE ON issue
FOR EACH ROW EXECUTE FUNCTION enqueue_knowledge_effect_dirty_for_issue_delete();

-- Trigger 4: issue BEFORE UPDATE OF project_id.
-- Re-attribute every bucket touched by tasks under this issue.
CREATE OR REPLACE FUNCTION enqueue_knowledge_effect_dirty_for_issue_project()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        -- OLD project buckets.
        INSERT INTO knowledge_effect_hourly_dirty (
            bucket_hour, workspace_id, agent_id, project_id,
            model, provider, task_kind, has_injection
        )
        SELECT DISTINCT
            knowledge_effect_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.agent_id,
            OLD.project_id,
            tu.model,
            tu.provider,
            compute_task_kind(atq.chat_session_id, atq.autopilot_run_id, atq.issue_id, atq.trigger_comment_id),
            inj.has_inj
        FROM agent_task_queue atq
        JOIN task_usage tu ON tu.task_id = atq.id
        CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
        WHERE atq.issue_id = NEW.id
          AND atq.status IN ('completed', 'failed')
        ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);

        -- NEW project buckets.
        INSERT INTO knowledge_effect_hourly_dirty (
            bucket_hour, workspace_id, agent_id, project_id,
            model, provider, task_kind, has_injection
        )
        SELECT DISTINCT
            knowledge_effect_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.agent_id,
            NEW.project_id,
            tu.model,
            tu.provider,
            compute_task_kind(atq.chat_session_id, atq.autopilot_run_id, atq.issue_id, atq.trigger_comment_id),
            inj.has_inj
        FROM agent_task_queue atq
        JOIN task_usage tu ON tu.task_id = atq.id
        CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
        WHERE atq.issue_id = NEW.id
          AND atq.status IN ('completed', 'failed')
        ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
            SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_issue_project_dirty_knowledge_effect
BEFORE UPDATE OF project_id ON issue
FOR EACH ROW EXECUTE FUNCTION enqueue_knowledge_effect_dirty_for_issue_project();

-- Trigger 5: task_usage BEFORE DELETE.
-- INSERT/UPDATE are covered by the updated_at watermark.
CREATE OR REPLACE FUNCTION enqueue_knowledge_effect_dirty_for_tu()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO knowledge_effect_hourly_dirty (
        bucket_hour, workspace_id, agent_id, project_id,
        model, provider, task_kind, has_injection
    )
    SELECT
        knowledge_effect_hour_bucket(OLD.created_at),
        a.workspace_id,
        atq.agent_id,
        i.project_id,
        OLD.model,
        OLD.provider,
        compute_task_kind(atq.chat_session_id, atq.autopilot_run_id, atq.issue_id, atq.trigger_comment_id),
        inj.has_inj
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    CROSS JOIN (VALUES (true), (false)) AS inj(has_inj)
    WHERE atq.id = OLD.task_id
      AND atq.status IN ('completed', 'failed')
    ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_dirty_key DO UPDATE
        SET enqueued_at = GREATEST(knowledge_effect_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE TRIGGER trg_tu_dirty_knowledge_effect
BEFORE DELETE ON task_usage
FOR EACH ROW EXECUTE FUNCTION enqueue_knowledge_effect_dirty_for_tu();

-- Window function. Mirrors 102's structure:
--   1. Discover dirty keys from the task_usage updated_at watermark + the queue.
--   2. Expand watermark-discovered keys into both has_injection values.
--   3. Recompute each key from agent_task_queue + task_usage + agent + issue.
--   4. Upsert; delete buckets that recomputed to nothing.
--   5. Drain the queue rows whose enqueued_at < p_to.
CREATE OR REPLACE FUNCTION rollup_knowledge_effect_hourly_window(
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
            knowledge_effect_hour_bucket(tu.created_at) AS bucket_hour,
            a.workspace_id                                AS workspace_id,
            atq.agent_id                                  AS agent_id,
            i.project_id                                  AS project_id,
            tu.model                                      AS model,
            tu.provider                                   AS provider,
            compute_task_kind(atq.chat_session_id, atq.autopilot_run_id, atq.issue_id, atq.trigger_comment_id) AS task_kind
        FROM task_usage tu
        JOIN agent_task_queue atq ON atq.id = tu.task_id
        JOIN agent a ON a.id = atq.agent_id
        LEFT JOIN issue i ON i.id = atq.issue_id
        WHERE atq.status IN ('completed', 'failed')
          AND (
               (tu.updated_at >= p_from AND tu.updated_at < p_to)
               OR (tu.updated_at IS NULL
                   AND tu.created_at >= p_from
                   AND tu.created_at <  p_to)
          )
    ),
    dirty_keys AS (
        SELECT bucket_hour, workspace_id, agent_id, project_id,
               model, provider, task_kind, inj.has_injection
        FROM dirty_from_updates d
        CROSS JOIN (VALUES (true), (false)) AS inj(has_injection)
        UNION
        SELECT bucket_hour, workspace_id, agent_id, project_id,
               model, provider, task_kind, has_injection
        FROM knowledge_effect_hourly_dirty
        WHERE enqueued_at < p_to
    ),
    recomputed AS (
        SELECT
            dk.bucket_hour,
            dk.workspace_id,
            dk.agent_id,
            dk.project_id,
            dk.model,
            dk.provider,
            dk.task_kind,
            dk.has_injection,
            COUNT(DISTINCT atq.id)::bigint AS task_count,
            COUNT(DISTINCT atq.id) FILTER (WHERE atq.status = 'completed')::bigint AS successful_count,
            COUNT(DISTINCT atq.id) FILTER (WHERE atq.status = 'failed')::bigint AS failed_count,
            COALESCE(SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))
                FILTER (WHERE atq.started_at IS NOT NULL AND atq.completed_at IS NOT NULL), 0)::double precision AS total_duration_secs,
            COUNT(DISTINCT atq.id) FILTER (WHERE atq.started_at IS NOT NULL AND atq.completed_at IS NOT NULL)::bigint AS duration_task_count,
            COALESCE(SUM(tu.input_tokens), 0)::bigint AS input_tokens,
            COALESCE(SUM(tu.output_tokens), 0)::bigint AS output_tokens,
            COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS cache_read_tokens,
            COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
            COUNT(DISTINCT atq.id) FILTER (WHERE atq.attempt > 1)::bigint AS rerun_count,
            COUNT(DISTINCT atq.id) FILTER (WHERE atq.parent_task_id IS NOT NULL)::bigint AS follow_up_count,
            COALESCE(MAX(atq.attempt), 1)::int AS max_attempt
        FROM dirty_keys dk
        JOIN agent_task_queue atq ON atq.agent_id = dk.agent_id
        JOIN task_usage tu ON tu.task_id = atq.id
            AND tu.model = dk.model
            AND tu.provider = dk.provider
            AND knowledge_effect_hour_bucket(tu.created_at) = dk.bucket_hour
        LEFT JOIN issue i ON i.id = atq.issue_id
        WHERE atq.status IN ('completed', 'failed')
          AND (i.project_id IS NOT DISTINCT FROM dk.project_id)
          AND compute_task_kind(atq.chat_session_id, atq.autopilot_run_id, atq.issue_id, atq.trigger_comment_id) = dk.task_kind
          AND EXISTS(
              SELECT 1 FROM knowledge_injection_event kie
              WHERE kie.agent_task_id = atq.id
          ) = dk.has_injection
        GROUP BY 1, 2, 3, 4, 5, 6, 7, 8
    ),
    upserted AS (
        INSERT INTO knowledge_effect_hourly AS d (
            bucket_hour, workspace_id, agent_id, project_id,
            model, provider, task_kind, has_injection,
            task_count, successful_count, failed_count,
            total_duration_secs, duration_task_count,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            rerun_count, follow_up_count, max_attempt
        )
        SELECT
            bucket_hour, workspace_id, agent_id, project_id,
            model, provider, task_kind, has_injection,
            task_count, successful_count, failed_count,
            total_duration_secs, duration_task_count,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            rerun_count, follow_up_count, max_attempt
        FROM recomputed
        ON CONFLICT ON CONSTRAINT uq_knowledge_effect_hourly_key DO UPDATE
            SET task_count          = EXCLUDED.task_count,
                successful_count    = EXCLUDED.successful_count,
                failed_count        = EXCLUDED.failed_count,
                total_duration_secs = EXCLUDED.total_duration_secs,
                duration_task_count = EXCLUDED.duration_task_count,
                input_tokens        = EXCLUDED.input_tokens,
                output_tokens       = EXCLUDED.output_tokens,
                cache_read_tokens   = EXCLUDED.cache_read_tokens,
                cache_write_tokens  = EXCLUDED.cache_write_tokens,
                rerun_count         = EXCLUDED.rerun_count,
                follow_up_count     = EXCLUDED.follow_up_count,
                max_attempt         = EXCLUDED.max_attempt,
                updated_at          = now()
        RETURNING 1
    ),
    deleted_empty AS (
        DELETE FROM knowledge_effect_hourly d
        USING dirty_keys dk
        WHERE d.bucket_hour   = dk.bucket_hour
          AND d.workspace_id  = dk.workspace_id
          AND d.agent_id      = dk.agent_id
          AND d.project_id IS NOT DISTINCT FROM dk.project_id
          AND d.model         = dk.model
          AND d.provider      = dk.provider
          AND d.task_kind     = dk.task_kind
          AND d.has_injection = dk.has_injection
          AND NOT EXISTS (
              SELECT 1 FROM recomputed r
              WHERE r.bucket_hour   = dk.bucket_hour
                AND r.workspace_id  = dk.workspace_id
                AND r.agent_id      = dk.agent_id
                AND r.project_id IS NOT DISTINCT FROM dk.project_id
                AND r.model         = dk.model
                AND r.provider      = dk.provider
                AND r.task_kind     = dk.task_kind
                AND r.has_injection = dk.has_injection
          )
        RETURNING 1
    )
    SELECT (SELECT COUNT(*) FROM upserted) + (SELECT COUNT(*) FROM deleted_empty)
    INTO v_rows;

    DELETE FROM knowledge_effect_hourly_dirty WHERE enqueued_at < p_to;

    RETURN v_rows;
END;
$$;

-- Dirty-queue TTL. The window function above drains rows on every tick.
-- This explicit prune is belt-and-braces for rows that escape a tick
-- (crash mid-tick, worker paused during incident/migration freeze).
-- 7-day retention matches the task_usage_hourly pattern.
CREATE OR REPLACE FUNCTION prune_knowledge_effect_hourly_dirty(
    p_retention INTERVAL DEFAULT INTERVAL '7 days'
)
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_rows BIGINT;
BEGIN
    DELETE FROM knowledge_effect_hourly_dirty
     WHERE enqueued_at < now() - p_retention;
    GET DIAGNOSTICS v_rows = ROW_COUNT;
    RETURN v_rows;
END;
$$;

-- Cron entry. Uses advisory lock 4248 (next available after 4246 task_usage,
-- 4247 reserved for future use).
CREATE OR REPLACE FUNCTION rollup_knowledge_effect_hourly()
RETURNS BIGINT
LANGUAGE plpgsql
AS $$
DECLARE
    v_lock_ok BOOLEAN;
    v_from    TIMESTAMPTZ;
    v_to      TIMESTAMPTZ;
    v_rows    BIGINT := 0;
BEGIN
    SELECT pg_try_advisory_lock(4248) INTO v_lock_ok;
    IF NOT v_lock_ok THEN
        RETURN 0;
    END IF;

    BEGIN
        UPDATE knowledge_effect_hourly_rollup_state
           SET last_run_started_at = now(),
               last_error          = NULL
         WHERE id = 1
        RETURNING watermark_at INTO v_from;

        v_to := LEAST(now() - INTERVAL '5 minutes', v_from + INTERVAL '1 day');

        IF v_from < v_to THEN
            v_rows := rollup_knowledge_effect_hourly_window(v_from, v_to);

            UPDATE knowledge_effect_hourly_rollup_state
               SET watermark_at         = v_to,
                   last_run_finished_at = now(),
                   last_run_rows        = v_rows
             WHERE id = 1;
        ELSE
            UPDATE knowledge_effect_hourly_rollup_state
               SET last_run_finished_at = now(),
                   last_run_rows        = 0
             WHERE id = 1;
        END IF;

        PERFORM pg_advisory_unlock(4248);
    EXCEPTION WHEN OTHERS THEN
        UPDATE knowledge_effect_hourly_rollup_state
           SET last_error           = SQLERRM,
               last_run_finished_at = now()
         WHERE id = 1;
        PERFORM pg_advisory_unlock(4248);
        RAISE;
    END;

    PERFORM prune_knowledge_effect_hourly_dirty();
    RETURN v_rows;
END;
$$;

-- Health-check helper.
CREATE OR REPLACE FUNCTION knowledge_effect_hourly_rollup_lag_seconds()
RETURNS DOUBLE PRECISION
LANGUAGE sql
STABLE
AS $$
    SELECT EXTRACT(EPOCH FROM (now() - last_run_finished_at))
      FROM knowledge_effect_hourly_rollup_state
     WHERE id = 1;
$$;
