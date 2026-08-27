ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS requested_concrete_model;
DROP TABLE IF EXISTS model_pricing;
DROP TABLE IF EXISTS model_health;
ALTER TABLE model_tier_map DROP COLUMN IF EXISTS fallback_concrete;
