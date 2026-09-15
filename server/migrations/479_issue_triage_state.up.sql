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
ALTER TABLE issue
    ADD COLUMN triage_state TEXT,
    ADD CONSTRAINT issue_triage_state_known CHECK (triage_state IS NULL OR triage_state IN ('pending'));
