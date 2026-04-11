CREATE TABLE workspace_sandbox_config (
    workspace_id UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    provider_api_key TEXT NOT NULL,
    ai_gateway_api_key TEXT,
    git_pat TEXT,
    template_id TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
