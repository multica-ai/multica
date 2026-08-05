DROP INDEX IF EXISTS idx_agent_unpause_due;

ALTER TABLE agent
    DROP COLUMN IF EXISTS auto_pause_count,
    DROP COLUMN IF EXISTS pause_reason,
    DROP COLUMN IF EXISTS unpause_at,
    DROP COLUMN IF EXISTS paused_at;
