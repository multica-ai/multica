-- FIR-4359: DELETE /api/autopilots/{id} hung for >15s on any autopilot with
-- run history, so the confirm dialog stayed stuck on "Deleting..." with Cancel
-- disabled and the row was never removed.
--
-- Deleting an autopilot cascades to its autopilot_run rows (042_autopilot).
-- Every deleted run then fires the ON DELETE SET NULL referential-integrity
-- trigger for model_usage_event.autopilot_run_id (9143_model_usage_event_ledger),
-- which runs "UPDATE model_usage_event SET autopilot_run_id = NULL WHERE
-- autopilot_run_id = $1". That column had no index, so each run cost one
-- sequential scan of the whole ledger — ~100 runs meant ~100 full scans.
--
-- Partial because the vast majority of ledger rows are not autopilot-driven
-- and carry NULL here; "autopilot_run_id = $1" is strict, so the planner can
-- still use the partial index for the RI lookup. Same shape as
-- idx_webhook_delivery_run in 093_webhook_deliveries.
--
-- CONCURRENTLY because model_usage_event is written on every model call; the
-- migration runner cannot mix CONCURRENTLY with other statements in one file,
-- so this is a single-statement migration (see 068/080).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_model_usage_event_autopilot_run
    ON model_usage_event (autopilot_run_id)
    WHERE autopilot_run_id IS NOT NULL;
