ALTER TABLE feishu_project_integration
    ADD COLUMN IF NOT EXISTS business_line_field_key  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS business_line_field_name TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS feishu_project_business_line_route (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    integration_id UUID NOT NULL REFERENCES feishu_project_integration(id) ON DELETE CASCADE,
    workspace_id   UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id     UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    business_line_id          TEXT NOT NULL,
    business_line_name        TEXT NOT NULL,
    parent_business_line_id   TEXT NOT NULL DEFAULT '',
    parent_business_line_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (integration_id, business_line_id)
);

CREATE INDEX IF NOT EXISTS idx_feishu_business_line_route_integration
    ON feishu_project_business_line_route(integration_id);
CREATE INDEX IF NOT EXISTS idx_feishu_business_line_route_workspace
    ON feishu_project_business_line_route(workspace_id);
CREATE INDEX IF NOT EXISTS idx_feishu_business_line_route_project
    ON feishu_project_business_line_route(project_id);
