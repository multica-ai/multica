ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS default_model_api_key,
    DROP COLUMN IF EXISTS default_model_config;
