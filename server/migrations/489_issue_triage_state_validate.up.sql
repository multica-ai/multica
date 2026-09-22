-- Validate the CHECK migration 483 added NOT VALID.
--
-- Split out so the scan runs under SHARE UPDATE EXCLUSIVE — readers and writers
-- continue — instead of inheriting migration 483's ACCESS EXCLUSIVE lock, which
-- would stop the issue table for the length of the scan. Migration 483 added
-- the column with no default and no backfill, so every row this scan reads is
-- NULL: the work is marking the constraint trusted, not repairing data.
ALTER TABLE issue VALIDATE CONSTRAINT issue_triage_state_known;
