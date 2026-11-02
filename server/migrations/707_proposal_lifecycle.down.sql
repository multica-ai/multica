-- Restore the three-state CHECK first: while proposals are revising or expired
-- this fails and the whole rollback is rolled back, which is the intent. Rows
-- are never rewritten here; the new columns are dropped only after the CHECK
-- holds.
ALTER TABLE proposal DROP CONSTRAINT IF EXISTS proposal_state_check;
ALTER TABLE proposal ADD CONSTRAINT proposal_state_check
    CHECK (state IN ('pending', 'approved', 'rejected'));

ALTER TABLE proposal DROP COLUMN IF EXISTS snoozed_until;
ALTER TABLE proposal DROP COLUMN IF EXISTS expires_at;
ALTER TABLE proposal DROP COLUMN IF EXISTS revision;
