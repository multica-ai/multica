ALTER TABLE agent_runtime
    ADD COLUMN hold_until TIMESTAMPTZ,
    ADD COLUMN hold_reason TEXT;
