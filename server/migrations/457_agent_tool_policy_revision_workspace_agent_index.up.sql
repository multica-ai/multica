CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_policy_revision_workspace_agent
    ON agent_tool_policy_revision (workspace_id, agent_id, revision DESC, id DESC);
