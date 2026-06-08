-- 9067_cerebro_workspace_connections: workspace-managed API and MCP connection registry.
--
-- Stores named connections (HTTP MCP servers, REST APIs) that workspace admins
-- configure once and all runtimes/agents can use. Each connection appears as a
-- capability row so the existing tool-policy chain controls access at every level.
--
-- auth_config stores bearer tokens, API keys, or CF Access credentials as JSON.
-- endpoint_permissions is a JSON array of {path, methods[]} objects used to drive
-- per-endpoint CRUD control in the permissions UI (TECH-3108).

CREATE TABLE IF NOT EXISTS workspace_connection (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name           TEXT        NOT NULL,
    display_name   TEXT        NOT NULL,
    type           TEXT        NOT NULL,
    url            TEXT        NOT NULL,
    internal       BOOLEAN     NOT NULL DEFAULT false,
    auth_config    JSONB       NOT NULL DEFAULT '{}',
    endpoint_permissions JSONB NOT NULL DEFAULT '[]',
    enabled        BOOLEAN     NOT NULL DEFAULT true,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workspace_connection_name_unique UNIQUE (workspace_id, name),
    CONSTRAINT workspace_connection_type_known CHECK (type IN ('mcp_http', 'api')),
    CONSTRAINT workspace_connection_name_not_blank CHECK (length(trim(name)) > 0),
    CONSTRAINT workspace_connection_url_not_blank  CHECK (length(trim(url))  > 0)
);

CREATE INDEX IF NOT EXISTS idx_workspace_connection_workspace
    ON workspace_connection (workspace_id, enabled, created_at);
