-- Reverse 120_runtime_profile.up.sql. No DB foreign keys were added by the up
-- migration (relationships are enforced in the application layer), so ordering
-- here only needs to drop dependent index/column before the table they live
-- alongside.

DROP INDEX IF EXISTS agent_runtime_workspace_daemon_profile_key;

ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS profile_id;

DROP INDEX IF EXISTS idx_runtime_profile_workspace;

DROP TABLE IF EXISTS runtime_profile;
