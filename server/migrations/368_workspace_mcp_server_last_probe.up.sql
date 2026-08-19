-- Last on-demand daemon probe of one workspace MCP library entry (GH #7166).
-- Write-only config stays in `config`; this column is the redacted summary
-- (status, runtime identity, tool names). No index: we never query by it.
ALTER TABLE workspace_mcp_server
    ADD COLUMN IF NOT EXISTS last_probe JSONB;
