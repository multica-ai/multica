CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_tool_policy_revision_agent_revision_uidx
    ON agent_tool_policy_revision (agent_id, revision);
