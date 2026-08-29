-- Local agent configuration: stores detected CLI installations and user overrides.
CREATE TABLE IF NOT EXISTS local_agent_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,             -- "claude", "codex", "opencode"
    cli_path TEXT NOT NULL DEFAULT '',  -- auto-detected or user-configured path
    version TEXT NOT NULL DEFAULT '',   -- detected CLI version
    status TEXT NOT NULL DEFAULT 'unknown', -- "available", "unavailable", "unknown"
    is_custom_path BOOLEAN NOT NULL DEFAULT false,
    last_health_check TIMESTAMPTZ,
    health_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, provider)
);

-- Local skills: filesystem-backed skills stored in DB for sync.
CREATE TABLE IF NOT EXISTS local_skill (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID REFERENCES workspace(id) ON DELETE CASCADE, -- NULL = global skill
    project_path TEXT,                  -- NULL = global, non-null = project-specific
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT false, -- bundled default skill
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index for looking up local skills by workspace or globally.
CREATE INDEX IF NOT EXISTS idx_local_skill_workspace ON local_skill(workspace_id);
CREATE INDEX IF NOT EXISTS idx_local_skill_project ON local_skill(project_path);
