-- Rollback: Remove agent_sessions table and indexes.
DROP INDEX IF EXISTS idx_sessions_run_number;
DROP INDEX IF EXISTS idx_sessions_expiry;
DROP INDEX IF EXISTS idx_sessions_active;
DROP TABLE IF EXISTS agent_sessions;
