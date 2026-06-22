ALTER TABLE agent_runtime
    DROP CONSTRAINT IF EXISTS agent_runtime_claim_window_pair,
    DROP COLUMN IF EXISTS claim_window_timezone,
    DROP COLUMN IF EXISTS claim_window_start;
