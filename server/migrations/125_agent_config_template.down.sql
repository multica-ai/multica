ALTER TABLE agent DROP COLUMN IF EXISTS skip_personal_template;
ALTER TABLE agent DROP COLUMN IF EXISTS skip_system_template;
ALTER TABLE agent DROP COLUMN IF EXISTS personal_template_id;
ALTER TABLE agent DROP COLUMN IF EXISTS system_template_id;
DROP TABLE IF EXISTS agent_config_template;
