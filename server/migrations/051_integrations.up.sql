-- Integration configuration per workspace.
CREATE TABLE workspace_integration (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('linear', 'github')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    config JSONB NOT NULL DEFAULT '{}',
    default_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    webhook_secret TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, provider)
);

-- Track mapping between external issues (Linear/GitHub) and Multica issues.
CREATE TABLE external_issue_link (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('linear', 'github')),
    external_id TEXT NOT NULL,
    external_identifier TEXT,
    external_url TEXT,
    last_synced_at TIMESTAMPTZ,
    sync_direction TEXT NOT NULL DEFAULT 'bidirectional'
        CHECK (sync_direction IN ('inbound', 'outbound', 'bidirectional')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, provider, external_id)
);

CREATE INDEX idx_external_issue_link_issue ON external_issue_link(issue_id);
CREATE INDEX idx_external_issue_link_lookup ON external_issue_link(workspace_id, provider, external_id);
CREATE INDEX idx_workspace_integration_workspace ON workspace_integration(workspace_id);
