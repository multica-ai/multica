ALTER TABLE agent_task_queue
DROP COLUMN IF EXISTS trigger_actor_id,
DROP COLUMN IF EXISTS trigger_actor_type,
DROP COLUMN IF EXISTS trigger_source;
