DROP INDEX IF EXISTS agent_task_queue_originator_user_id_idx;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS originator_user_id;
ALTER TABLE agent DROP COLUMN IF EXISTS composio_toolkit_allowlist;
