-- Reverse 121_agent_runtime_provider_partial_unique.up.sql.
--
-- The up migration is additive and deliberately keeps the legacy
-- agent_runtime_workspace_id_daemon_id_provider_key constraint for rolling
-- deploy/rollback compatibility. Rolling this migration back only removes the
-- prepared partial index.

DROP INDEX IF EXISTS agent_runtime_workspace_daemon_provider_key;
