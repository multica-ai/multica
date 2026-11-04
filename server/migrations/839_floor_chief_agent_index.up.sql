-- Finds the floors an agent chairs (the brief publish check and the agent's
-- own checks).
CREATE INDEX CONCURRENTLY IF NOT EXISTS floor_chief_agent_idx ON floor_chief (workspace_id, agent_id);
