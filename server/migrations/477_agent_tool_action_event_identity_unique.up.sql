ALTER TABLE agent_tool_action_event
    ADD CONSTRAINT agent_tool_action_event_identity_key
    UNIQUE USING INDEX agent_tool_action_event_identity_uidx;
