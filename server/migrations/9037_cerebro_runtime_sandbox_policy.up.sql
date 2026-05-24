ALTER TABLE agent_runtime
ADD COLUMN IF NOT EXISTS sandbox_policy jsonb NOT NULL DEFAULT '{}'::jsonb;
