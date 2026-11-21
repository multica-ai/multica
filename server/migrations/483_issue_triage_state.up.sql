-- Move Triage off `issue.status` and onto the issue's own attribute
-- (MUL-7213, design MUL-7189).
--
-- `status` answers "how far along is this work"; Triage answers "is this work
-- at all". Holding the second question in the first field makes every reader of
-- `status` re-answer it, which is where the scattered `<> 'triage'` checks came
-- from. A column of its own asks it once.
--
-- NULL is an ordinary issue and is the whole meaning of "not in Triage", so no
-- backfill and no default: adding a nullable column with no default is a
-- catalog-only change in PostgreSQL 11+, so this does not rewrite the table.
--
-- `pending` is the only state Triage can be in today. The outcomes a triager
-- records (declined, merged into another issue) are their own sub-issue and
-- extend this CHECK when they land; keeping the constraint narrow now is what
-- makes that a deliberate change rather than a silent typo.
--
-- The CHECK is added NOT VALID: a validated CHECK scans every existing issue
-- while holding the ALTER's ACCESS EXCLUSIVE lock, which stops all issue reads
-- and writes for the length of that scan even though the new column needs no
-- backfill. NOT VALID enforces the constraint on every write from this point
-- on and skips the scan; migration 489 validates the rows already on disk under
-- SHARE UPDATE EXCLUSIVE, which readers and writers run straight through.
--
-- The runner sends this file as one implicit transaction. Bound lock
-- acquisition and execution so this catalog-only change fails fast and retries
-- on the next run rather than parking a pending ACCESS EXCLUSIVE lock in front
-- of every issue query.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE issue
    ADD COLUMN triage_state TEXT,
    ADD CONSTRAINT issue_triage_state_known CHECK (triage_state IS NULL OR triage_state IN ('pending')) NOT VALID;
