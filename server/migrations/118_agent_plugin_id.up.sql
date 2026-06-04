-- 118_agent_plugin_id.up.sql
-- Add plugin_id column to multica_agent for external plugin binding.
ALTER TABLE multica_agent ADD COLUMN plugin_id TEXT;
