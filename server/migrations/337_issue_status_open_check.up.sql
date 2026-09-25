-- Open `issue.status` to catalog keys (MUL-6243).
--
-- The old constraint hard-coded the 7 built-in statuses, which is exactly the
-- thing a per-workspace catalog has to be able to extend. Membership validation
-- moves to the application layer, where it is checked against issue_status for
-- the issue's OWN workspace — something a table-level CHECK cannot express.
--
-- A format constraint stays behind as defense in depth: it cannot know whether
-- a key exists in the catalog, but it does keep an empty string or arbitrary
-- text out of the column if a write path ever forgets to validate. Added
-- NOT VALID so it takes no table scan under lock; migration 338 validates it.
ALTER TABLE issue DROP CONSTRAINT issue_status_check;

ALTER TABLE issue
    ADD CONSTRAINT issue_status_format_check
    CHECK (status ~ '^[a-z0-9][a-z0-9_]{0,31}$')
    NOT VALID;
