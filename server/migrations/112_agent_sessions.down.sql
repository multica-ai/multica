DROP TRIGGER IF EXISTS trg_agent_sessions_last_active_at ON agent_sessions;
DROP FUNCTION IF EXISTS update_agent_sessions_last_active_at();
DROP INDEX IF EXISTS idx_sessions_state_gin;
DROP INDEX IF EXISTS idx_sessions_run_number;
DROP INDEX IF EXISTS idx_sessions_expiry;
DROP INDEX IF EXISTS idx_sessions_active;
DROP TABLE IF EXISTS agent_sessions;
