CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_tool_policy_rule_identity_uidx
    ON agent_tool_policy_rule (agent_id, transport_kind, server_key, tool_name, schema_digest);
