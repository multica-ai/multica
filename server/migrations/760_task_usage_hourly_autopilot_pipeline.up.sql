-- Switch the task_usage_hourly rollup pipeline and dirty-key triggers to the
-- autopilot_id-aware unique indexes built concurrently in migrations 758–759.
--
-- A run's autopilot is COALESCE(autopilot_run.autopilot_id of the task,
-- issue.origin_id when issue.origin_type = 'autopilot'): run-only autopilots
-- carry their run on the task; create-issue autopilots are found through the
-- issue they created, including later runs on that issue.
--
-- The 8-column squad unique indexes are dropped in THIS transaction so the
-- 9-column ON CONFLICT statements never meet a stricter unique index. 761–762
-- are forward no-ops whose down directions rebuild them CONCURRENTLY before
-- 760 down restores the squad-only pipeline.
--
-- Dirty keys carrying an autopilot_id also enqueue the same key with
-- autopilot_id = NULL (and, as before, squad_id = NULL), so buckets rolled up
-- before 757 heal when a key is recomputed instead of double-counting.
--
-- Deleting an autopilot cascades to its runs, and the runs' tasks lose
-- autopilot_run_id through ON DELETE SET NULL after the run row is gone. The
-- new trg_autopilot_run_delete_dirty_hourly enqueues the run's old keys while
-- the run still exists; the task update then enqueues the new key.

DROP INDEX IF EXISTS uq_task_usage_hourly_squad_key_idx;
DROP INDEX IF EXISTS uq_task_usage_hourly_dirty_squad_key_idx;

CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_atq()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.runtime_id IS DISTINCT FROM NEW.runtime_id
           OR OLD.issue_id IS DISTINCT FROM NEW.issue_id
           OR OLD.autopilot_run_id IS DISTINCT FROM NEW.autopilot_run_id
           OR COALESCE(OLD.usage_squad_id, OLD.squad_id) IS DISTINCT FROM COALESCE(NEW.usage_squad_id, NEW.squad_id) THEN
            IF OLD.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, squad_id, autopilot_id, provider, model
                )
                SELECT DISTINCT
                    task_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    OLD.runtime_id,
                    OLD.agent_id,
                    i_old.project_id,
                    COALESCE(OLD.usage_squad_id, OLD.squad_id),
                    COALESCE(ar_old.autopilot_id, CASE WHEN i_old.origin_type = 'autopilot' THEN i_old.origin_id END),
                    tu.provider,
                    tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = OLD.agent_id
                  LEFT JOIN issue i_old ON i_old.id = OLD.issue_id
                  LEFT JOIN autopilot_run ar_old ON ar_old.id = OLD.autopilot_run_id
                 WHERE tu.task_id = OLD.id
                ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;

            IF NEW.runtime_id IS NOT NULL THEN
                INSERT INTO task_usage_hourly_dirty (
                    bucket_hour, workspace_id, runtime_id, agent_id,
                    project_id, squad_id, autopilot_id, provider, model
                )
                SELECT DISTINCT
                    task_usage_hour_bucket(tu.created_at),
                    a.workspace_id,
                    NEW.runtime_id,
                    NEW.agent_id,
                    i_new.project_id,
                    COALESCE(NEW.usage_squad_id, NEW.squad_id),
                    COALESCE(ar_new.autopilot_id, CASE WHEN i_new.origin_type = 'autopilot' THEN i_new.origin_id END),
                    tu.provider,
                    tu.model
                  FROM task_usage tu
                  JOIN agent a ON a.id = NEW.agent_id
                  LEFT JOIN issue i_new ON i_new.id = NEW.issue_id
                  LEFT JOIN autopilot_run ar_new ON ar_new.id = NEW.autopilot_run_id
                 WHERE tu.task_id = NEW.id
                ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
                    SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
            END IF;
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        IF OLD.runtime_id IS NOT NULL THEN
            INSERT INTO task_usage_hourly_dirty (
                bucket_hour, workspace_id, runtime_id, agent_id,
                project_id, squad_id, autopilot_id, provider, model
            )
            SELECT DISTINCT
                task_usage_hour_bucket(tu.created_at),
                a.workspace_id,
                OLD.runtime_id,
                OLD.agent_id,
                i.project_id,
                COALESCE(OLD.usage_squad_id, OLD.squad_id),
                COALESCE(ar.autopilot_id, CASE WHEN i.origin_type = 'autopilot' THEN i.origin_id END),
                tu.provider,
                tu.model
              FROM task_usage tu
              JOIN agent a ON a.id = OLD.agent_id
              LEFT JOIN issue i ON i.id = OLD.issue_id
              LEFT JOIN autopilot_run ar ON ar.id = OLD.autopilot_run_id
             WHERE tu.task_id = OLD.id
            ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
                SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
        END IF;
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_atq_dirty_hourly ON agent_task_queue;
CREATE TRIGGER trg_atq_dirty_hourly
BEFORE UPDATE OF runtime_id, issue_id, squad_id, usage_squad_id, autopilot_run_id OR DELETE ON agent_task_queue
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION enqueue_task_usage_hourly_dirty_for_atq();

CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_issue_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, squad_id, autopilot_id, provider, model
    )
    SELECT DISTINCT
        task_usage_hour_bucket(tu.created_at),
        OLD.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        OLD.project_id,
        COALESCE(atq.usage_squad_id, atq.squad_id),
        COALESCE(ar.autopilot_id, CASE WHEN OLD.origin_type = 'autopilot' THEN OLD.origin_id END),
        tu.provider,
        tu.model
      FROM agent_task_queue atq
      JOIN task_usage tu ON tu.task_id = atq.id
      LEFT JOIN autopilot_run ar ON ar.id = atq.autopilot_run_id
     WHERE atq.issue_id = OLD.id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
        SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_issue_project()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.project_id IS DISTINCT FROM NEW.project_id THEN
        INSERT INTO task_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, squad_id, autopilot_id, provider, model
        )
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            OLD.project_id,
            COALESCE(atq.usage_squad_id, atq.squad_id),
            COALESCE(ar.autopilot_id, CASE WHEN OLD.origin_type = 'autopilot' THEN OLD.origin_id END),
            tu.provider,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage tu ON tu.task_id = atq.id
          LEFT JOIN autopilot_run ar ON ar.id = atq.autopilot_run_id
         WHERE atq.issue_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
            SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);

        INSERT INTO task_usage_hourly_dirty (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, squad_id, autopilot_id, provider, model
        )
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at),
            NEW.workspace_id,
            atq.runtime_id,
            atq.agent_id,
            NEW.project_id,
            COALESCE(atq.usage_squad_id, atq.squad_id),
            COALESCE(ar.autopilot_id, CASE WHEN NEW.origin_type = 'autopilot' THEN NEW.origin_id END),
            tu.provider,
            tu.model
          FROM agent_task_queue atq
          JOIN task_usage tu ON tu.task_id = atq.id
          LEFT JOIN autopilot_run ar ON ar.id = atq.autopilot_run_id
         WHERE atq.issue_id = NEW.id
           AND atq.runtime_id IS NOT NULL
        ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
            SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_tu()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, squad_id, autopilot_id, provider, model
    )
    SELECT
        task_usage_hour_bucket(OLD.created_at),
        a.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        i.project_id,
        COALESCE(atq.usage_squad_id, atq.squad_id),
        COALESCE(ar.autopilot_id, CASE WHEN i.origin_type = 'autopilot' THEN i.origin_id END),
        OLD.provider,
        OLD.model
      FROM agent_task_queue atq
      JOIN agent a ON a.id = atq.agent_id
      LEFT JOIN issue i ON i.id = atq.issue_id
      LEFT JOIN autopilot_run ar ON ar.id = atq.autopilot_run_id
     WHERE atq.id = OLD.task_id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
        SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

-- An autopilot run's tasks keep their usage but lose the run (ON DELETE SET
-- NULL) once the run row is gone, too late for the task trigger to see which
-- autopilot the old bucket belonged to. Enqueue those keys here.
CREATE OR REPLACE FUNCTION enqueue_task_usage_hourly_dirty_for_autopilot_run_delete()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO task_usage_hourly_dirty (
        bucket_hour, workspace_id, runtime_id, agent_id,
        project_id, squad_id, autopilot_id, provider, model
    )
    SELECT DISTINCT
        task_usage_hour_bucket(tu.created_at),
        a.workspace_id,
        atq.runtime_id,
        atq.agent_id,
        i.project_id,
        COALESCE(atq.usage_squad_id, atq.squad_id),
        OLD.autopilot_id,
        tu.provider,
        tu.model
      FROM agent_task_queue atq
      JOIN agent a ON a.id = atq.agent_id
      JOIN task_usage tu ON tu.task_id = atq.id
      LEFT JOIN issue i ON i.id = atq.issue_id
     WHERE atq.autopilot_run_id = OLD.id
       AND atq.runtime_id IS NOT NULL
    ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
        SET enqueued_at = GREATEST(task_usage_hourly_dirty.enqueued_at, EXCLUDED.enqueued_at);
    RETURN OLD;
END;
$$;

DROP TRIGGER IF EXISTS trg_autopilot_run_delete_dirty_hourly ON autopilot_run;
CREATE TRIGGER trg_autopilot_run_delete_dirty_hourly
BEFORE DELETE ON autopilot_run
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION enqueue_task_usage_hourly_dirty_for_autopilot_run_delete();

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
    dirty_raw AS (
        SELECT DISTINCT
            task_usage_hour_bucket(tu.created_at)      AS bucket_hour,
            a.workspace_id                             AS workspace_id,
            atq.runtime_id                             AS runtime_id,
            atq.agent_id                               AS agent_id,
            i.project_id                               AS project_id,
            COALESCE(atq.usage_squad_id, atq.squad_id) AS squad_id,
            COALESCE(ar.autopilot_id, CASE WHEN i.origin_type = 'autopilot' THEN i.origin_id END) AS autopilot_id,
            tu.provider                                AS provider,
            tu.model                                   AS model
          FROM task_usage tu
          JOIN agent_task_queue atq ON atq.id      = tu.task_id
          JOIN agent            a   ON a.id        = atq.agent_id
          LEFT JOIN issue       i   ON i.id        = atq.issue_id
          LEFT JOIN autopilot_run ar ON ar.id      = atq.autopilot_run_id
         WHERE atq.runtime_id IS NOT NULL
           AND (
                (tu.updated_at >= p_from AND tu.updated_at < p_to)
                OR (tu.updated_at IS NULL
                    AND tu.created_at >= p_from
                    AND tu.created_at <  p_to)
           )
    ),
    dirty_queue_raw AS (
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, squad_id, autopilot_id, provider, model
          FROM task_usage_hourly_dirty
         WHERE enqueued_at < p_to
    ),
    dirty_attributed AS (
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, squad_id, autopilot_id, provider, model
          FROM dirty_raw
        UNION
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, squad_id, autopilot_id, provider, model
          FROM dirty_queue_raw
    ),
    dirty_keys AS (
        SELECT * FROM dirty_attributed
        UNION
        -- Heal buckets rolled up before 757 (autopilot_id = NULL) whenever an
        -- autopilot-attributed key in that bucket is recomputed.
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, squad_id, NULL::uuid AS autopilot_id, provider, model
          FROM dirty_attributed
         WHERE autopilot_id IS NOT NULL
        UNION
        -- Heal buckets rolled up before 735 (squad_id = NULL, and so also
        -- autopilot_id = NULL) the same way.
        SELECT bucket_hour, workspace_id, runtime_id, agent_id,
               project_id, NULL::uuid AS squad_id, NULL::uuid AS autopilot_id, provider, model
          FROM dirty_attributed
         WHERE squad_id IS NOT NULL OR autopilot_id IS NOT NULL
    ),
    recomputed AS (
        SELECT
            dk.bucket_hour,
            dk.workspace_id,
            dk.runtime_id,
            dk.agent_id,
            dk.project_id,
            dk.squad_id,
            dk.autopilot_id,
            dk.provider,
            dk.model,
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
          FROM dirty_keys dk
          JOIN agent_task_queue atq ON atq.runtime_id  = dk.runtime_id
                                    AND atq.agent_id    = dk.agent_id
                                    AND COALESCE(atq.usage_squad_id, atq.squad_id) IS NOT DISTINCT FROM dk.squad_id
          JOIN agent            a   ON a.id            = atq.agent_id
                                    AND a.workspace_id = dk.workspace_id
          LEFT JOIN issue       i   ON i.id            = atq.issue_id
          LEFT JOIN autopilot_run ar ON ar.id          = atq.autopilot_run_id
          JOIN task_usage       tu  ON tu.task_id      = atq.id
                                    AND tu.provider    = dk.provider
                                    AND tu.model       = dk.model
                                    AND task_usage_hour_bucket(tu.created_at) = dk.bucket_hour
         WHERE (i.project_id IS NOT DISTINCT FROM dk.project_id)
           AND COALESCE(ar.autopilot_id, CASE WHEN i.origin_type = 'autopilot' THEN i.origin_id END) IS NOT DISTINCT FROM dk.autopilot_id
         GROUP BY 1, 2, 3, 4, 5, 6, 7, 8, 9
    ),
    upserted AS (
        INSERT INTO task_usage_hourly AS d (
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, squad_id, autopilot_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
        )
        SELECT
            bucket_hour, workspace_id, runtime_id, agent_id,
            project_id, squad_id, autopilot_id, provider, model,
            input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
            cost_usd_ticks,
            uncosted_input_tokens, uncosted_output_tokens,
            uncosted_cache_read_tokens, uncosted_cache_write_tokens,
            task_count, event_count
          FROM recomputed
        ON CONFLICT (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model) DO UPDATE
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
         USING dirty_keys dk
         WHERE d.bucket_hour  = dk.bucket_hour
           AND d.workspace_id = dk.workspace_id
           AND d.runtime_id   = dk.runtime_id
           AND d.agent_id     = dk.agent_id
           AND d.project_id   IS NOT DISTINCT FROM dk.project_id
           AND d.squad_id     IS NOT DISTINCT FROM dk.squad_id
           AND d.autopilot_id IS NOT DISTINCT FROM dk.autopilot_id
           AND d.provider     = dk.provider
           AND d.model        = dk.model
           AND NOT EXISTS (
               SELECT 1 FROM recomputed r
                WHERE r.bucket_hour  = dk.bucket_hour
                  AND r.workspace_id = dk.workspace_id
                  AND r.runtime_id   = dk.runtime_id
                  AND r.agent_id     = dk.agent_id
                  AND r.project_id   IS NOT DISTINCT FROM dk.project_id
                  AND r.squad_id     IS NOT DISTINCT FROM dk.squad_id
                  AND r.autopilot_id IS NOT DISTINCT FROM dk.autopilot_id
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
