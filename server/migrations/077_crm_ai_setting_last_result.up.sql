ALTER TABLE crm_ai_setting ADD COLUMN IF NOT EXISTS last_result JSONB NOT NULL DEFAULT '{}'::jsonb;
