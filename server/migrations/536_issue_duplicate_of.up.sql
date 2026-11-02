-- Record which issue a cancelled issue duplicates (MUL-7349).
--
-- A duplicate is an ordinary cancelled issue with a pointer to the issue it
-- duplicates; there is no duplicate status. The pointer only lives while the
-- issue is cancelled: a write that leaves an already cancelled issue cancelled
-- keeps it, and every other write drops it. Reopening an issue is therefore how
-- a mark is removed, and cancelling it again does not bring the mark back.
--
-- This is the expand half of the feature: the column ships with the code that
-- maintains it, and nothing writes a pointer yet. A later release adds marking.
-- That order is what makes marking safe to roll back — the release it rolls
-- back to already clears the pointer on reopen and on delete, so no issue is
-- left carrying a mark the product no longer honours.
--
-- No foreign key (repository rule). Deleting the original clears the pointers
-- to it in the same transaction, the way deleting a parent detaches children.
--
-- A nullable column with no default is a catalog-only change, so this does not
-- rewrite the table. Bound lock acquisition so the ALTER fails fast and retries
-- on the next run instead of queueing an ACCESS EXCLUSIVE lock in front of
-- every issue query.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE issue ADD COLUMN IF NOT EXISTS duplicate_of_issue_id UUID;
