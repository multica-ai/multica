CREATE TABLE feishu_project_integration (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_key TEXT NOT NULL,
    plugin_id TEXT NOT NULL,
    plugin_secret TEXT NOT NULL,
    actor_user_key TEXT,
    enabled BOOLEAN NOT NULL DEFAULT true,
    sync_story BOOLEAN NOT NULL DEFAULT true,
    sync_issue BOOLEAN NOT NULL DEFAULT true,
    mql_filter TEXT NOT NULL DEFAULT 'is_archived = 0',
    status_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
    reverse_status_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    last_synced_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_key)
);

CREATE INDEX idx_feishu_project_integration_workspace
    ON feishu_project_integration(workspace_id);

CREATE TABLE feishu_project_issue_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    integration_id UUID NOT NULL REFERENCES feishu_project_integration(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    project_key TEXT NOT NULL,
    work_item_type TEXT NOT NULL,
    work_item_id TEXT NOT NULL,
    external_identifier TEXT NOT NULL,
    external_url TEXT,
    external_status_label TEXT,
    last_external_updated_at TIMESTAMPTZ,
    last_synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (integration_id, work_item_type, work_item_id),
    UNIQUE (workspace_id, issue_id)
);

CREATE INDEX idx_feishu_project_issue_binding_issue
    ON feishu_project_issue_binding(issue_id);

CREATE TABLE feishu_project_sync_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    integration_id UUID NOT NULL REFERENCES feishu_project_integration(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    trigger TEXT NOT NULL,
    created_count INTEGER NOT NULL DEFAULT 0,
    updated_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    error_count INTEGER NOT NULL DEFAULT 0,
    error TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX idx_feishu_project_sync_run_integration
    ON feishu_project_sync_run(integration_id, started_at DESC);
