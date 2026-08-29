ALTER TABLE agent_tool_policy_revision
    ADD CONSTRAINT agent_tool_policy_revision_agent_revision_key
    UNIQUE USING INDEX agent_tool_policy_revision_agent_revision_uidx;
