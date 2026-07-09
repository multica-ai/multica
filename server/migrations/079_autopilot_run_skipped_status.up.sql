-- MUL-1899: re-introduce the 'skipped' terminal status for autopilot_run.
-- Migration 043 removed 'skipped' along with the broken concurrency_policy
-- feature, but the offline-runtime admission gate added in this PR needs a
-- non-failure terminal status to record dispatches that were intentionally
-- declined (e.g. assignee runtime is offline). Reusing 'failed' would
-- pollute the failure-rate signal that drives the auto-pause monitor.
ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('issue_created', 'running', 'completed', 'failed', 'skipped'));

-- Partial index on status for in-flight runs is unchanged: 'skipped' is
-- terminal so the existing index (issue_created/running) still matches.
--
-- The companion partial index for the queued-task TTL sweeper lives in
-- migration 080 — it must be created CONCURRENTLY (hot table) and therefore
-- cannot share a multi-statement file with the constraint change above.
