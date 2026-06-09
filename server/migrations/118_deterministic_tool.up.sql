-- Workspace-authored deterministic tools ("steps"): user-written Go that runs in
-- the sandboxed interpreter and is exposed to agents through the deterministic
-- tool plane alongside the built-in compiled tools.
CREATE TABLE deterministic_tool (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- name doubles as the MCP tool name the agent calls, so it must be unique
    -- within a workspace.
    UNIQUE (workspace_id, name)
);

CREATE INDEX idx_deterministic_tool_workspace ON deterministic_tool(workspace_id);
