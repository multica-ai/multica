DROP TRIGGER IF EXISTS trg_agent_sessions_last_active_at ON agent_sessions;
DROP FUNCTION IF EXISTS update_agent_sessions_last_active_at();
DROP TABLE IF EXISTS agent_sessions CASCADE;
