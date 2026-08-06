ALTER TABLE agent ADD COLUMN IF NOT EXISTS persona_sandbox TEXT;
ALTER TABLE agent_runtime ADD COLUMN IF NOT EXISTS persona_sandbox TEXT;
