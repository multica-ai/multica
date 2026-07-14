-- Intentionally a no-op. Migration 181 repairs unsafe historical forms of
-- migration 180. Rolling it back must not restore the broad issue-table
-- unique constraint or the workspace-composite foreign key. Migration 180's
-- down migration remains responsible for removing the external identity
-- table and migration-owned triggers/functions when rolling back farther.
SELECT 1;
