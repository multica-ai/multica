ALTER TABLE agent DROP COLUMN IF EXISTS personal_template_id;
ALTER TABLE agent DROP COLUMN IF EXISTS system_template_id;
DROP TABLE IF EXISTS agent_config_template;
