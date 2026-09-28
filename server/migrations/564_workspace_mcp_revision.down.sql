DROP TRIGGER IF EXISTS workspace_mcp_revision_before_update ON workspace_mcp_server;
DROP FUNCTION IF EXISTS advance_workspace_mcp_revision();
ALTER TABLE workspace_mcp_server DROP COLUMN IF EXISTS revision;
