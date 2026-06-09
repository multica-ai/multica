ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS hold_until,
    DROP COLUMN IF EXISTS hold_reason;
