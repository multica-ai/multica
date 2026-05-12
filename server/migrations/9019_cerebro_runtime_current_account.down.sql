DROP INDEX IF EXISTS idx_agent_runtime_current_account;

ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS current_account_id;
