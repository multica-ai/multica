ALTER TABLE agent_tool_policy_rule
    ADD CONSTRAINT agent_tool_policy_rule_identity_key
    UNIQUE USING INDEX agent_tool_policy_rule_identity_uidx;
