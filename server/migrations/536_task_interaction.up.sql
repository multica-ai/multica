-- Run-owned control state is embedded so ordinary task deletion also removes
-- its commands, without adding foreign keys or a second cleanup authority.
ALTER TABLE agent_task_queue ADD COLUMN interaction jsonb;
