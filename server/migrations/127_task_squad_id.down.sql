DROP INDEX IF EXISTS agent_task_queue_squad_id_idx;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS squad_id;
