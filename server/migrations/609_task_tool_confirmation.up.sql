-- Pending tool-call approvals raised by an external runtime (Qoder Cloud
-- Agents `requires_action`). Relationships intentionally have no foreign
-- keys: task, issue and workspace teardown remain application-owned.
CREATE TABLE task_tool_confirmation (
    task_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    issue_id UUID,
    call_id TEXT NOT NULL,
    tool_name TEXT NOT NULL,
    mcp_server_name TEXT,
    input JSONB,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'allowed', 'denied')),
    deny_message TEXT,
    decided_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at TIMESTAMPTZ
);
