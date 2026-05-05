-- Project Resources: typed pointers from a project to external resources
-- (github_repo for now; notion_page / gdoc / url / file later). The shape is
-- intentionally polymorphic — resource_type is a free string and resource_ref
-- is JSONB, so adding a new type requires zero schema changes.
CREATE TABLE project_resource (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_ref  JSONB NOT NULL,
    label         TEXT,
    position      INT  NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by    UUID,
    UNIQUE (project_id, resource_type, resource_ref)
);

CREATE INDEX idx_project_resource_project ON project_resource(project_id, position);
CREATE INDEX idx_project_resource_workspace ON project_resource(workspace_id);
