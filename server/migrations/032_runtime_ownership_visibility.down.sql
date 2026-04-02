ALTER TABLE agent_runtime DROP CONSTRAINT agent_runtime_visibility_check;
ALTER TABLE agent_runtime DROP COLUMN visibility;
ALTER TABLE agent_runtime DROP COLUMN owner_id;
