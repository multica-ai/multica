-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- task_token.agent_id has been a NOT NULL FK since migration 108 with no index.
-- Two consumers need one:
--
--   1. Workspace teardown deletes tokens by agent as one of the three explicit
--      task_token paths (MUL-5999). It must not fall back on the agent_id
--      cascade, because the legacy FKs are documented as an expand-phase safety
--      net that a later contract removes.
--   2. The FK's own ON DELETE CASCADE probe, which PostgreSQL runs once per
--      deleted agent row — a full scan of task_token per agent without an index,
--      on the ordinary agent-delete path too.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_token_agent_id
    ON task_token (agent_id);
