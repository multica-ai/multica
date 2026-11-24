DROP FUNCTION task_runtime_allowed(UUID, UUID, JSONB);
ALTER TABLE agent_task_queue DROP COLUMN runtime_routing;
DROP TABLE agent_runtime_preference;
