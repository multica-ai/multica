-- One row per MCP action the platform itself observed.
--
-- This is the audit log. It is not plugin_invocation (operational telemetry,
-- 7-day TTL, no arguments) and not task_message (the transcript, kept with
-- the run). Result bodies stay out: Codex already drops MCP result content
-- from the transcript, and a second copy would keep private tool output
-- after an admin clears the run log.
--
-- No foreign keys (repo rule). Linkage is copied at write time so the row
-- outlives the task. Workspace teardown deletes these rows explicitly.
CREATE TABLE mcp_action_audit (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL,
    task_id             UUID NOT NULL,
    agent_id            UUID NOT NULL,
    issue_id            UUID,
    chat_session_id     UUID,
    originator_user_id  UUID,
    provider            TEXT,
    source              TEXT NOT NULL
        CHECK (source IN ('provider_stream', 'remote_broker', 'plugin_hook')),
    mcp_server          TEXT,
    tool_name           TEXT NOT NULL CHECK (char_length(tool_name) BETWEEN 1 AND 256),
    call_id             TEXT,
    arguments           JSONB,
    outcome             TEXT NOT NULL
        CHECK (outcome IN ('started', 'ok', 'error', 'denied', 'timeout', 'unavailable')),
    error               TEXT CHECK (error IS NULL OR char_length(error) <= 500),
    duration_ms         INTEGER CHECK (duration_ms IS NULL OR duration_ms >= 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_mcp_action_audit_workspace_created
    ON mcp_action_audit (workspace_id, created_at DESC);

CREATE INDEX idx_mcp_action_audit_created
    ON mcp_action_audit (created_at);

-- A retried transcript batch or broker report for the same call updates the
-- row instead of inserting another. NULL call ids stay distinct: plugin-hook
-- attempts do not share an id.
CREATE UNIQUE INDEX uq_mcp_action_audit_call
    ON mcp_action_audit (task_id, source, call_id)
    WHERE call_id IS NOT NULL AND call_id <> '';
