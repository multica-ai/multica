-- Workspace MCP servers as an explicit per-agent assignment (GH #6062,
-- MUL-5421), replacing the inherit-by-default document added in 314.
--
-- A workspace-level library of MCP servers plus an explicit per-agent binding,
-- deliberately shaped like `skill` + `agent_skill`: creating a server here
-- gives it to NOBODY. It reaches an agent only when someone adds it to that
-- agent, and the binding carries its own on/off toggle. Nothing about this
-- layer propagates implicitly — that is the whole point of the shape, and the
-- reason 314's model is being replaced rather than extended.
--
-- No foreign keys (repo rule): the application sweeps `agent_mcp_server` when
-- an agent or a server goes away, and both tables on workspace delete.
CREATE TABLE workspace_mcp_server (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    -- One MCP server entry: the object that sits under a name in
    -- `mcpServers`. Secret-bearing (urls, headers, env), never returned by the
    -- API — see the write-only contract in workspace_mcp_api.go.
    config JSONB NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agent_mcp_server (
    agent_id UUID NOT NULL,
    server_id UUID NOT NULL,
    -- Bound but switched off: the agent keeps the association and the toggle
    -- state, exactly like agent_skill.enabled, so turning it back on does not
    -- mean finding the server again.
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, server_id)
);
