-- Make the hourly rollup's advisory lock transaction-scoped.
--
-- Migration 102 took lock 4246 with the SESSION-scoped pg_try_advisory_lock
-- and released it explicitly on both the success path and an
-- `EXCEPTION WHEN OTHERS` path. Two PostgreSQL rules combine to make that
-- leak the lock forever:
--
--   1. OTHERS does not match query_canceled. A cancelled tick — statement
--      timeout, pg_cancel_backend, or pgx cancelling the query when the Go
--      context is done (the scheduler caps a tick at 25 min) — skips the
--      handler entirely, so pg_advisory_unlock never runs.
--   2. A session-level advisory lock does not honour transaction semantics:
--      it survives the rollback that follows.
--
-- pgxpool then hands the connection back to the pool still holding 4246, and
-- nothing releases it until that connection is recycled or the process exits.
--
-- The consequences are silent and unbounded: every later tick loses
-- pg_try_advisory_lock and returns 0 ("no work to do"), so usage aggregation
-- stops, and DeleteWorkspace — which waits on pg_advisory_xact_lock(4246) to
-- keep the rollup out of a workspace it is tearing down — blocks forever, so
-- deleting a workspace appears to do nothing at all (MUL-5983).
--
-- pg_try_advisory_xact_lock is released by the transaction end, including a
-- rollback from a cancelled statement, so no failure mode can leak it. The
-- function is called as a plain `SELECT rollup_task_usage_hourly()`, i.e. its
-- own implicit transaction, so the lock still covers exactly one tick.
--
-- The one behaviour change: the TTL prune at the end used to run after an
-- explicit unlock and now runs inside the locked transaction, so whatever it
-- costs is added to the window in which 4246 is unavailable — to the next
-- tick, to a backfill run, and to a workspace delete, which now waits on 4246
-- under a 10 s budget and answers 503 when it runs out. The prune is an
-- index-driven DELETE (task_usage_hourly_dirty.enqueued_at) with no row cap:
-- in steady state each tick already drains the queue and it deletes nothing,
-- but after a long pause it has a real backlog to clear. Watch rollup duration
-- and workspace-delete 503s together if that ever happens.
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
    SELECT pg_try_advisory_xact_lock(4246) INTO v_lock_ok;
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
    EXCEPTION WHEN OTHERS THEN
        UPDATE task_usage_hourly_rollup_state
           SET last_error           = SQLERRM,
               last_run_finished_at = now()
         WHERE id = 1;
        RAISE;
    END;

    -- TTL prune. Idempotent, and in steady state a no-op because each tick
    -- already drains the queue. It runs under the transaction-scoped lock —
    -- see the note at the top for what that costs after a long pause.
    PERFORM prune_task_usage_hourly_dirty();
    RETURN v_rows;
END;
$$;
