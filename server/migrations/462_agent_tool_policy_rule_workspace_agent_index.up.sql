CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_policy_rule_workspace_agent
    ON agent_tool_policy_rule (workspace_id, agent_id, transport_kind, server_key, tool_name);
