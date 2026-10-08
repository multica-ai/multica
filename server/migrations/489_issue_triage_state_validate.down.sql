-- PostgreSQL cannot mark a validated constraint NOT VALID again, so recreate
-- the state migration 483 left: enforcing every new write, not yet validated.
-- Both statements are catalog-only, so the ACCESS EXCLUSIVE lock is held only
-- long enough to swap the catalog rows.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_triage_state_known;
ALTER TABLE issue ADD CONSTRAINT issue_triage_state_known
    CHECK (triage_state IS NULL OR triage_state IN ('pending')) NOT VALID;
