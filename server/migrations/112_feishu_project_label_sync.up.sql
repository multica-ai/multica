ALTER TABLE feishu_project_integration
    ADD COLUMN IF NOT EXISTS label_sync_rules JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE IF NOT EXISTS feishu_project_label_sync_binding (
    integration_id UUID NOT NULL REFERENCES feishu_project_integration(id) ON DELETE CASCADE,
    workspace_id   UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id       UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    rule_id        TEXT NOT NULL,
    label_id       UUID NOT NULL REFERENCES issue_label(id) ON DELETE CASCADE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (integration_id, issue_id, rule_id)
);

CREATE INDEX IF NOT EXISTS idx_feishu_project_label_sync_binding_issue
    ON feishu_project_label_sync_binding(integration_id, issue_id);
