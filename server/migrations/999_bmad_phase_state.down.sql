-- Down: drop BMAD additions. Leaves base issue.status CHECK only if something upstream recreates it.
DROP INDEX IF EXISTS idx_issue_phase_state_phase;
DROP INDEX IF EXISTS idx_issue_phase_state_status;
ALTER TABLE issue DROP COLUMN IF EXISTS phase_state;
