-- Add previous_failure_reason to autopilot_run so that when a run
-- transitions from failed → completed (e.g. issue-status recovery
-- after a task timeout false-failure, MYW-1917), the original
-- failure_reason is preserved for audit.
ALTER TABLE autopilot_run ADD COLUMN IF NOT EXISTS previous_failure_reason TEXT;
