-- Drop the `triage` status-key reservation wherever it was applied (MUL-7400).
--
-- Migrations 475-477 are emptied by this change, which is enough for every
-- database that has not run them yet — production included, since releases
-- deploy from tags and no tag carries them. A database that did migrate off
-- main keeps the CHECK, and would refuse a custom status keyed `triage` that
-- the server now accepts: the reservation is gone from issuestatus, so the
-- write would fail on the constraint instead of being answered.
--
-- IF EXISTS makes this a no-op on the first group and the repair on the second.
-- Dropping a constraint is catalog-only; the timeouts bound the lock the same
-- way migration 478 does, in the same implicit transaction the runner gives
-- this file.
--
-- 476's rename is not reversed: a workspace whose custom `triage` became
-- `triage_2` keeps it, because the replacement is an ordinary custom key its
-- admin may already have seen, and handing `triage` back would rewrite live
-- data a second time to undo the first.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE issue_status DROP CONSTRAINT IF EXISTS issue_status_key_not_reserved;
