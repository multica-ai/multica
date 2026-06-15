DROP INDEX IF EXISTS idx_agent_runtime_hold_until;

ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS hold_until,
    DROP COLUMN IF EXISTS hold_reason;
