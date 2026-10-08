-- Admin approval for an mcp-transport hook's tool list.
--
-- A hook with transport.type == "mcp" points at an MCP server the plugin author
-- already runs, and Multica adopts its tools as callable hooks. Unlike an http
-- hook — where the manifest declares exactly one endpoint and one shape — an MCP
-- server decides its own tool list at runtime and can change it whenever it
-- likes.
--
-- That is the difference this column exists for. An administrator approves a
-- specific list with a specific schema digest; a tool that appears later is not
-- adopted, and a tool whose schema drifts stops being called. Without the pin,
-- "install this plugin" would be a standing grant to run whatever that server
-- decides to offer next week.
--
-- Keyed by hook key, so one installation can contribute several mcp hooks and
-- each is approved on its own:
--   {"<hook_key>": {"tools": [{"name": ..., "schema_digest": ...}],
--                   "approved_at": "...", "approved_by": "<user uuid>"}}
--
-- JSONB rather than a table: nothing joins on it, nothing queries inside it, and
-- it is read as a whole whenever the installation is. A table would be a second
-- lifecycle to keep in step with the installation for no reader's benefit.
ALTER TABLE plugin_installation
    ADD COLUMN IF NOT EXISTS mcp_approvals JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(mcp_approvals) = 'object');
