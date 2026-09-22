-- Migration 511 (SE-37711 / SE-37664): durable evidence for an automatic
-- runtime failover on a run_only autopilot dispatch.
--
-- When the breaker-aware pool selector routes a task to a runtime other than the
-- agent's default (a quota fallover), or dispatches a half-open probe, the
-- reassignment must never be silent: the task's runtime_id records WHERE it ran,
-- and this column records WHY it moved as structured evidence that task/run
-- detail surfaces. One task per (autopilot_run, target runtime), so the row is
-- naturally idempotent — a single audit object per run+target.
--
-- Nullable with no default: an ordinary dispatch (no failover, no probe) writes
-- nothing here, so the column stays NULL and adds no cost to the common path.
-- The daemon later merges the per-execution model fail-safe outcome
-- (model_action) into the same object via jsonb_set at task pickup.
ALTER TABLE agent_task_queue
  ADD COLUMN IF NOT EXISTS dispatch_runtime_audit jsonb;
