-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a multi-command migration file.
--
-- Step 1 of the two-step primary key. Building the index concurrently first
-- means the constraint in 268 only has to adopt it, never build it under an
-- ACCESS EXCLUSIVE lock.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS inbox_group_pkey_idx
    ON inbox_group (id);
