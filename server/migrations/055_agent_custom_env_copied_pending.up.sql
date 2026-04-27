ALTER TABLE agent
ADD COLUMN IF NOT EXISTS custom_env_copied_pending boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN agent.custom_env_copied_pending IS 'True when env vars were copied without secret values; user must fill values before use.';
