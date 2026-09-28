-- Preserve the unique fence even if an operator rolls back only this migration.
-- Migration 564 drops the table (and its index) only when it is empty. A live
-- claim must never be left in a table without the issue-wide unique fence.
SELECT 1;
