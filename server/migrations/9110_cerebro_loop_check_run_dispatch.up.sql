-- FIR-2283 loop check transport (dispatch). The engine enqueues a check as a
-- pending row (loops.Store.Enqueue); this column lets it then send that check
-- out to the worker agent's runtime exactly once. A row is enqueued with
-- dispatched_at NULL, the dispatcher posts the check argv to the runtime as a
-- task and stamps dispatched_at. A later re-evaluation of the same gate skips
-- rows that already carry a dispatched_at, so a check in flight is never
-- re-sent. Dispatch state is kept separate from the run result (ran/exit_code)
-- because a check can be dispatched yet not have reported back yet.
ALTER TABLE cerebro_loop_check_run
    ADD COLUMN IF NOT EXISTS dispatched_at TIMESTAMPTZ;
