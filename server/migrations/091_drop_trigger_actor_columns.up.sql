-- Drop the trigger_actor columns from agent_task_queue.
-- These were added by migration 057_agent_task_trigger_actor but the
-- daemon-side permission gate that consumed them has been removed.
-- The columns are no longer written or read by any code path.
ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS trigger_actor_id,
  DROP COLUMN IF EXISTS trigger_actor_type,
  DROP COLUMN IF EXISTS trigger_source;
