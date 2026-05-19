-- 9032_cerebro_runtime_tool_grants: per-runtime tool registry + access grants.
--
-- JEH-1710. The admin UI presents a unified view of every tool a runtime exposes
-- (cloud-side built-ins + MCP servers discovered via tools/list) with per-tool
-- on/off, group access and user access. Agents inherit the runtime's grants;
-- a per-agent override table lets a workspace admin disable (or rarely enable)
-- a tool for a single agent.
--
-- Tables:
--   cerebro_runtime_tool                 — registry row per (runtime, tool).
--                                          enabled=true means the runtime
--                                          publishes the tool; group/user
--                                          grants control who may invoke it.
--   cerebro_runtime_tool_group_grant     — group has access to (runtime, tool).
--   cerebro_runtime_tool_user_grant      — user has access to (runtime, tool).
--   cerebro_agent_runtime_tool_override  — per-agent override of the runtime
--                                          default. enabled=false disables an
--                                          otherwise-granted tool for the
--                                          agent; enabled=true forces it on.
--
-- Access rule (default deny):
--   tool callable for (user, agent) iff
--     runtime_tool.enabled = true
--     AND ( exists matching user_grant
--           OR exists matching group_grant for a group the user is in
--           OR user is workspace owner/admin )
--     AND ( no override row OR override.enabled = true )
--
-- The 'source' column carries 'cloud' for in-process cloud tools (registered
-- via runtime.Registry.Register) and 'mcp' for tools discovered through an
-- MCP server's tools/list. mcp_server_name is NULL for cloud tools.
--
-- agent_runtime.tools_config (9031) is preserved: it still stores how each
-- MCP server is launched (Claude --mcp-config format). The new tables track
-- which TOOLS each server exposes and who may call them.

CREATE TABLE IF NOT EXISTS cerebro_runtime_tool (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    source TEXT NOT NULL,
    mcp_server_name TEXT,
    description TEXT,
    schema_json JSONB,
    enabled BOOLEAN NOT NULL DEFAULT false,
    last_scanned_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_runtime_tool_source_known CHECK (source IN ('cloud', 'mcp')),
    CONSTRAINT cerebro_runtime_tool_unique UNIQUE (runtime_id, tool_name, mcp_server_name)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_runtime_tool_runtime
    ON cerebro_runtime_tool (runtime_id);

CREATE TABLE IF NOT EXISTS cerebro_runtime_tool_group_grant (
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    group_id UUID NOT NULL REFERENCES cerebro_group(id) ON DELETE CASCADE,
    granted_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (runtime_id, tool_name, group_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_runtime_tool_group_grant_group
    ON cerebro_runtime_tool_group_grant (group_id);

CREATE TABLE IF NOT EXISTS cerebro_runtime_tool_user_grant (
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    granted_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (runtime_id, tool_name, user_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_runtime_tool_user_grant_user
    ON cerebro_runtime_tool_user_grant (user_id);

CREATE TABLE IF NOT EXISTS cerebro_agent_runtime_tool_override (
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, tool_name)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_runtime_tool_override_agent
    ON cerebro_agent_runtime_tool_override (agent_id);
