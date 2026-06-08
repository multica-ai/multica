-- MUL-2863: durable autopilot cron dispatch when the assignee agent's
-- runtime is offline at dispatch time. Adds a `pending_runtime` terminal-
-- state-equivalent status for autopilot_run and a column that records which
-- runtime the run is queued behind.
--
-- Without this, the MUL-1899 admission gate records a `skipped` run whenever
-- the runtime is offline — silently losing scheduled work for any user whose
-- laptop sleeps, restarts, or loses connectivity at the cron fire time. The
-- new status lets the cron tick persist a run record that surfaces in the UI
-- as "Waiting for agent runtime to come online" and is auto-dispatched the
-- next time the runtime/daemon reappears, instead of being thrown away.
--
-- We do NOT change the existing 'skipped' status semantics — it still
-- represents an admission failure that we are deliberately dropping (e.g.
-- the assignee agent was hard-deleted, or the squad is archived). 'pending'
-- from the original 042 schema is reused for the same intent but the
-- constraint is being tightened to make it explicit.

ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('pending_runtime', 'issue_created', 'running', 'completed', 'failed', 'skipped'));

-- pending_runtime_id records which runtime the run is queued behind. Set
-- at admission time when we transition the run to 'pending_runtime';
-- cleared on transition to a non-pending status.
--
-- No FK to agent_runtime(id) on purpose: the runtime row can be GC'd
-- (offlineRuntimeTTLSeconds in cmd/server/runtime_sweeper.go) while a run
-- is still pending, and the run should keep surfacing in the UI as
-- "Waiting for runtime" until the user manually re-triggers or the
-- autopilot is re-pointed. Surfacing it without a runtime row also gives
-- ops a paper trail for "this autopilot never recovered" cases.
ALTER TABLE autopilot_run ADD COLUMN IF NOT EXISTS pending_runtime_id UUID;

-- Targeted partial index for the runtime-comes-online dispatch path
-- (ListPendingRuntimeAutopilotRunsForRuntime). Pending rows are
-- short-lived relative to completed/failed runs, so the partial
-- condition keeps the index lean. Oldest-first ordering matches the
-- queue semantics: when a runtime comes back online we dispatch the
-- oldest eligible pending run first.
CREATE INDEX IF NOT EXISTS idx_autopilot_run_pending_runtime
    ON autopilot_run(pending_runtime_id, triggered_at ASC)
    WHERE status = 'pending_runtime';

-- Tighten the in-flight partial index from migration 042 to also include
-- pending_runtime. The existing predicate (pending, issue_created, running)
-- predates the new status, so a pending run would not show up in the index
-- the sweeper / dispatcher uses to walk in-flight work. We DROP+CREATE here
-- rather than ALTER because the predicate changed.
DROP INDEX IF EXISTS idx_autopilot_run_status;
CREATE INDEX IF NOT EXISTS idx_autopilot_run_status
    ON autopilot_run(autopilot_id, status)
    WHERE status IN ('pending_runtime', 'issue_created', 'running');
