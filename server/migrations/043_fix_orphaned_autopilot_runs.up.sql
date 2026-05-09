-- Remove concurrency_policy from autopilot (was broken: skip had orphan bug,
-- queue didn't actually queue, replace didn't cancel running tasks).

-- 1. Clean up orphaned runs (issue deleted → issue_id NULL, status stuck).
UPDATE autopilot_run
SET status = 'failed',
    completed_at = now(),
    failure_reason = 'linked issue was deleted'
WHERE status = 'issue_created'
  AND issue_id IS NULL;

-- 2. Migrate skipped/pending runs to failed (these statuses are removed).
UPDATE autopilot_run
SET status = 'failed',
    completed_at = COALESCE(completed_at, now()),
    failure_reason = COALESCE(failure_reason, 'migrated from legacy status')
WHERE status IN ('skipped', 'pending');

-- 3. Update the status CHECK constraint to remove skipped and pending.
ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('issue_created', 'running', 'completed', 'failed'));

-- 4. Drop concurrency_policy column.
ALTER TABLE autopilot DROP COLUMN IF EXISTS concurrency_policy;

-- 5. Update the partial index on status to match new allowed values.
DROP INDEX IF EXISTS idx_autopilot_run_status;
CREATE INDEX IF NOT EXISTS idx_autopilot_run_status ON autopilot_run(autopilot_id, status)
    WHERE status IN ('issue_created', 'running');

-- 6. Add index for issue-linked run lookups (used by FailAutopilotRunsByIssue
--    and GetAutopilotRunByIssue before issue deletion).
CREATE INDEX IF NOT EXISTS idx_autopilot_run_issue ON autopilot_run(issue_id)
    WHERE issue_id IS NOT NULL;
