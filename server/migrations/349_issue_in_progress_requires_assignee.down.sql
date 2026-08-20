-- Drops the in_progress-requires-assignee guard. The legacy-zombie repair
-- from the up direction is deliberately NOT reversed: moving repaired rows
-- back to in_progress would recreate the exact zombies the constraint
-- exists to prevent. Down only removes the constraint, so the schema returns
-- to the pre-349 state while the repaired rows stay repaired.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_in_progress_requires_assignee_check;
