-- Reverse lookup for the sweep that runs when a workspace server is deleted
-- (and for "which agents use this server" later). The forward direction is
-- already covered by the (agent_id, server_id) primary key.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_mcp_server_server
    ON agent_mcp_server (server_id);
