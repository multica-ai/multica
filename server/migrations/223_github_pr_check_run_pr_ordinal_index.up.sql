-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a multi-command migration file. Split out from the table
-- creation in 222 so no index is built non-concurrently (CLAUDE.md rule applies
-- even to indexes on newly created tables, including a PRIMARY KEY's).
--
-- (pr_id, ordinal) is unique per snapshot — the atomic replace deletes all rows
-- for a pr_id then re-inserts with sequential ordinals — and this index also
-- serves the pr_id-prefix lookups used by the list aggregation and the
-- workspace/PR cleanup deletes.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS github_pull_request_check_run_pr_ordinal_idx
    ON github_pull_request_check_run (pr_id, ordinal);
