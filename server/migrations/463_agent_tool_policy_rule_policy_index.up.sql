CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_policy_rule_workspace_policy
    ON agent_tool_policy_rule (workspace_id, policy_id, id);
