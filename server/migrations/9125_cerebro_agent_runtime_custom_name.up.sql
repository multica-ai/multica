-- CEREBRO-PATCH(runtime-custom-name): optional user-facing runtime display override.
ALTER TABLE agent_runtime ADD COLUMN IF NOT EXISTS custom_name TEXT;
