-- Member lookup for agent_user_runtime_config rows when listing one user's
-- saved configurations across agents.
CREATE INDEX CONCURRENTLY idx_agent_user_runtime_config_user ON agent_user_runtime_config(user_id);
