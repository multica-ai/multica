-- Custom Runtime, PR2 (registration extension). See MUL-3284 / GitHub #3667.
--
-- PR1 (migration 120) added agent_runtime.profile_id and a partial unique index
-- for custom-runtime instances, but deliberately left the legacy
-- UNIQUE (workspace_id, daemon_id, provider) constraint intact so the existing
-- registration upsert kept working. That non-partial constraint blocks a
-- built-in runtime (profile_id IS NULL) and a custom runtime of the SAME
-- protocol family (provider) from coexisting on one daemon.
--
-- PR2 makes registration profile-aware, so we now convert that legacy key into
-- a PARTIAL unique index scoped to built-in rows only (profile_id IS NULL).
-- Combined with 120's partial index on (workspace_id, daemon_id, profile_id)
-- WHERE profile_id IS NOT NULL, this lets a single daemon host the built-in
-- codex AND any number of custom codex-based profiles without collision, while
-- still enforcing one built-in runtime per (workspace, daemon, provider).
--
-- The matching upserts now spell out the predicate in their ON CONFLICT
-- arbiter (see pkg/db/queries/runtime.sql): built-in registration targets
-- (workspace_id, daemon_id, provider) WHERE profile_id IS NULL; custom
-- registration targets (workspace_id, daemon_id, profile_id) WHERE
-- profile_id IS NOT NULL.

ALTER TABLE agent_runtime
    DROP CONSTRAINT agent_runtime_workspace_id_daemon_id_provider_key;

CREATE UNIQUE INDEX agent_runtime_workspace_daemon_provider_key
    ON agent_runtime (workspace_id, daemon_id, provider)
    WHERE profile_id IS NULL;
