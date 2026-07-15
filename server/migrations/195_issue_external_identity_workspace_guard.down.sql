-- Intentionally a no-op. Migration 195 repairs unsafe historical forms of
-- migration 194. Rolling it back must not restore the broad issue-table
-- unique constraint or the workspace-composite foreign key. Migration 194's
-- down migration remains responsible for removing the external identity
-- table and migration-owned triggers/functions when rolling back farther.
SELECT 1;
