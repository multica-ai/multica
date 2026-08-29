CREATE TABLE agent_tool_policy_rule (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    policy_id UUID NOT NULL,
    transport_kind TEXT NOT NULL CHECK (transport_kind IN ('managed_mcp', 'managed_native')),
    server_key TEXT NOT NULL CHECK (char_length(server_key) BETWEEN 1 AND 255),
    tool_name TEXT NOT NULL CHECK (char_length(tool_name) BETWEEN 1 AND 255),
    schema_digest TEXT NOT NULL CHECK (schema_digest ~ '^sha256:[0-9a-f]{64}$'),
    effect TEXT NOT NULL CHECK (effect IN ('allow', 'require_approval')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (server_key = btrim(server_key)),
    CHECK (tool_name = btrim(tool_name))
);
