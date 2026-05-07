-- CEREBRO-PATCH(migration-idempotent-046-agent-mcp-config): cerebro modification of upstream file
ALTER TABLE agent ADD COLUMN IF NOT EXISTS mcp_config jsonb;
