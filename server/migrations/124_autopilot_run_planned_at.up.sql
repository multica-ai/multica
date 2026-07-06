-- Adds planned_at + a partial unique constraint on (trigger_id, planned_at) to
-- autopilot_run. The column anchors each run to the canonical UTC planned fire
-- time of its triggering occurrence; the unique index is the dispatch-layer
-- idempotency guard for scheduled triggers.
--
-- Why we need it (MUL-3551):
--
--   * The primary idempotency for scheduled dispatch lives one layer up, in
--     `sys_cron_executions` (job_name, scope_kind, scope_id, plan_time). A
--     successful claim on that table is what gates running the handler.
--   * BUT: if a runner claims a plan_time, starts dispatch (creating an
--     autopilot_run row + an issue + an agent_task_queue row), then dies
--     before writing terminal SUCCESS, the row goes stale → a sibling runner
--     can steal the lease and re-enter the handler with the SAME plan_time.
--     Without a row-level guard the second attempt would create a duplicate
--     autopilot_run / issue / task.
--   * This index makes that double-create a primary-key conflict, so the
--     stale-steal path can detect "this plan_time already produced a run"
--     and reuse it instead of creating a duplicate.
--
-- Partial-unique is correct here because:
--
--   * Manual triggers and webhook triggers leave planned_at NULL (they have
--     no canonical fire time). The constraint must not apply to them.
--   * Pre-existing autopilot_run rows are all (planned_at IS NULL) — no
--     backfill needed, no migration risk.
--
-- The DEFAULT NULL keeps the existing trigger paths (manual / webhook / api
-- source) untouched; only DispatchAutopilotForPlan writes a non-NULL value.

ALTER TABLE autopilot_run
    ADD COLUMN planned_at TIMESTAMPTZ;

CREATE UNIQUE INDEX uq_autopilot_run_trigger_planned
    ON autopilot_run (trigger_id, planned_at)
    WHERE trigger_id IS NOT NULL AND planned_at IS NOT NULL;
