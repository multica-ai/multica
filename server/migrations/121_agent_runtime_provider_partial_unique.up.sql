-- Custom Runtime, PR2 compatibility stage. See MUL-3284 / GitHub #3667.
--
-- Migration 120 added agent_runtime.profile_id and the custom-runtime partial
-- unique index on (workspace_id, daemon_id, profile_id) WHERE profile_id IS NOT
-- NULL, while deliberately retaining the legacy UNIQUE (workspace_id, daemon_id,
-- provider) constraint. Old server builds still use:
--
--   ON CONFLICT (workspace_id, daemon_id, provider)
--
-- That statement requires a non-partial unique/exclusion arbiter on exactly
-- those columns at plan time. Dropping the legacy constraint in the same
-- release as profile-aware registration would break rolling deploys and
-- rollbacks: any old API pod that registers a daemon runtime after this
-- migration lands would fail before it can execute the DO UPDATE path.
--
-- This migration is therefore intentionally additive. It prepares the built-in
-- runtime partial unique index used by the new profile-aware upsert, but keeps
-- the legacy full constraint in place for old binaries. The full constraint
-- still prevents built-in and custom runtimes of the same provider from
-- coexisting on one daemon during this compatibility stage; a later, separately
-- released migration may drop agent_runtime_workspace_id_daemon_id_provider_key
-- after every running server build has stopped using the old conflict target.

CREATE UNIQUE INDEX IF NOT EXISTS agent_runtime_workspace_daemon_provider_key
    ON agent_runtime (workspace_id, daemon_id, provider)
    WHERE profile_id IS NULL;
