-- 9070_cerebro_connection_tools: persist the discovered tool list per connection.
--
-- TECH-3156. Until now the "Test connection" call returned the MCP server's
-- tools/list result transiently to the frontend and threw it away. The
-- permissions UI needs a durable list so it can render one Allow/Ask/Deny row
-- per underlying tool (e.g. customer-service exposes draft_reply, lookup_order,
-- search_knowledge, …) instead of a single row for the whole connection.
--
-- tools is a JSON array of {name, description} objects, refreshed whenever the
-- connection is (re)tested. The per-tool permission decisions themselves live in
-- cerebro_tool_policy (tool_key = 'connection:<name>', resource_pattern = <tool>),
-- mirroring how repos store per-repo decisions — so the five-level inheritance
-- chain is reused unchanged.

ALTER TABLE workspace_connection
    ADD COLUMN IF NOT EXISTS tools JSONB NOT NULL DEFAULT '[]';
