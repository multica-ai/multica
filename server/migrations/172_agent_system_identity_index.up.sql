-- This is intentionally a single statement: concurrent index creation cannot
-- run in a transaction or a multi-command migration.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_system_identity_unique
    ON agent (workspace_id, owner_id, runtime_id, system_key)
    WHERE system_key IS NOT NULL;
