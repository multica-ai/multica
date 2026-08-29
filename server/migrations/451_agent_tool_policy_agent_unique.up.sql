ALTER TABLE agent_tool_policy
    ADD CONSTRAINT agent_tool_policy_agent_key UNIQUE USING INDEX agent_tool_policy_agent_uidx;
