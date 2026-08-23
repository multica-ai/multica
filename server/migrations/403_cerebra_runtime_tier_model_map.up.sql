ALTER TABLE agent_runtime
    ADD COLUMN IF NOT EXISTS tier_model_map JSONB;
