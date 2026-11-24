ALTER TABLE agent_runtime_preference
    ADD COLUMN model_mode TEXT NOT NULL DEFAULT 'inherit' CHECK (model_mode IN ('inherit', 'runtime_default', 'custom')),
    ADD COLUMN model TEXT NOT NULL DEFAULT '',
    ADD COLUMN max_concurrent_tasks INTEGER CHECK (max_concurrent_tasks BETWEEN 1 AND 100),
    ADD CONSTRAINT personal_execution_model CHECK ((model_mode = 'custom' AND length(trim(model)) BETWEEN 1 AND 256) OR (model_mode <> 'custom' AND model = ''));
