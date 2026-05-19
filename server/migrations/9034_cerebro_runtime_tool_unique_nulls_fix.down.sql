ALTER TABLE cerebro_runtime_tool
    DROP CONSTRAINT IF EXISTS cerebro_runtime_tool_unique;

ALTER TABLE cerebro_runtime_tool
    ADD CONSTRAINT cerebro_runtime_tool_unique
    UNIQUE (runtime_id, tool_name, mcp_server_name);
