ALTER TABLE agent
ADD COLUMN disabled_runtime_skills JSONB NOT NULL DEFAULT '[]'::jsonb;
