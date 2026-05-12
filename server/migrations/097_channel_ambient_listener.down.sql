DROP INDEX IF EXISTS idx_channel_ambient_listener;
ALTER TABLE channel DROP COLUMN IF EXISTS ambient_listener_agent_id;
