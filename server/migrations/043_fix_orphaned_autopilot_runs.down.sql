-- Drop the issue_id index added in the up migration.
DROP INDEX IF EXISTS idx_autopilot_run_issue;

-- Restore the original partial status index.
DROP INDEX IF EXISTS idx_autopilot_run_status;
CREATE INDEX IF NOT EXISTS idx_autopilot_run_status ON autopilot_run(autopilot_id, status)
    WHERE status IN ('pending', 'issue_created', 'running');

-- Restore concurrency_policy column.
ALTER TABLE autopilot ADD COLUMN IF NOT EXISTS concurrency_policy TEXT NOT NULL DEFAULT 'skip'
    CHECK (concurrency_policy IN ('skip', 'queue', 'replace'));

-- Restore the original status CHECK constraint.
ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_status_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_status_check
    CHECK (status IN ('pending', 'issue_created', 'running', 'skipped', 'completed', 'failed'));
