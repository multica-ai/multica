CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_tool_policy_agent_uidx
    ON agent_tool_policy (agent_id);
