ALTER TABLE issue
    DROP CONSTRAINT IF EXISTS issue_triage_state_known,
    DROP COLUMN IF EXISTS triage_state;
