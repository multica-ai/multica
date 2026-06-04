-- 118_agent_plugin_id.down.sql
ALTER TABLE multica_agent DROP COLUMN IF EXISTS plugin_id;
