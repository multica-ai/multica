-- Reverse 121_agent_runtime_provider_partial_unique.up.sql.
--
-- Restoring the non-partial constraint requires that no two rows share
-- (workspace_id, daemon_id, provider). Custom-runtime rows (profile_id IS NOT
-- NULL) can violate that if a built-in and a custom runtime of the same
-- provider coexist on one daemon, so a clean downgrade assumes such rows have
-- been removed first (PR2's feature being rolled back). The DROP INDEX is
-- unconditional; the ADD CONSTRAINT will fail loudly if duplicates remain,
-- which is the correct, non-silent behavior for a rollback.

DROP INDEX IF EXISTS agent_runtime_workspace_daemon_provider_key;

ALTER TABLE agent_runtime
    ADD CONSTRAINT agent_runtime_workspace_id_daemon_id_provider_key
        UNIQUE (workspace_id, daemon_id, provider);
