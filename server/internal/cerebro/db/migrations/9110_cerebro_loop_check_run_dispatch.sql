-- FIR-2283 loop check transport (dispatch) sqlc schema input. Mirrors
-- server/migrations/9110_cerebro_loop_check_run_dispatch.up.sql so the schema
-- sqlc reads stays in sync with the runtime migration.
ALTER TABLE cerebro_loop_check_run
    ADD COLUMN IF NOT EXISTS dispatched_at TIMESTAMPTZ;
