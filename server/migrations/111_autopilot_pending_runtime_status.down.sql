-- Down migration for MUL-2863. Drops the pending_runtime-related objects.
-- Note: any 'pending_runtime' rows are migrated to 'skipped' with a
-- migration reason so the (post-079) constraint is still satisfied when
-- we re-tighten it below. This matches the 079 down approach: tighten
-- the constraint by walking the data through it, not by deleting rows.

UPDATE autopilot_run
SET status = 'skipped',
    completed_at = COALESCE(completed_at, now()),
    failure_reason = COALESCE(failure_reason, 'migrated from pending_runtime status')
WHERE status = 'pending_runtime';

-- Restore the original in-flight partial index predicate (the
-- 'pending_runtime' state is no longer valid after this migration lands).
DROP INDEX IF EXISTS idx_autopilot_run_pending_runtime;
DROP INDEX IF EXISTS idx_autopilot_run_status;
CREATE INDEX IF NOT EXISTS idx_autopilot_run_status
    ON autopilot_run(autopilot_id, status)
    WHERE status IN ('issue_created', 'running');

ALTER TABLE autopilot_run DROP COLUMN IF EXISTS pending_runtime_id;

ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('issue_created', 'running', 'completed', 'failed', 'skipped'));
