DROP TABLE IF EXISTS agent_available_space;
ALTER TABLE agent DROP CONSTRAINT IF EXISTS agent_workspace_id_id_unique;
ALTER TABLE agent DROP COLUMN IF EXISTS availability_mode;
