ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS current_account_id UUID NULL
        REFERENCES cerebro_account(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_agent_runtime_current_account
    ON agent_runtime (current_account_id);
