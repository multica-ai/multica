DROP INDEX IF EXISTS idx_agent_runtime_unpause_due;

ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS pause_reason,
    DROP COLUMN IF EXISTS unpause_at,
    DROP COLUMN IF EXISTS paused_at;
