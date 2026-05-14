CREATE TABLE IF NOT EXISTS agent_tool_grant (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,
  config_json JSONB,
  enabled BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(agent_id, tool_name)
);
