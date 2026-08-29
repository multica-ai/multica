CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_policy_workspace_agent
    ON agent_tool_policy (workspace_id, agent_id);
