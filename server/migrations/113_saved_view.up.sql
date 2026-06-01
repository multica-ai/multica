CREATE TABLE saved_view (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    creator_id   UUID REFERENCES "user"(id) ON DELETE SET NULL,
    name         TEXT NOT NULL,
    page         TEXT NOT NULL CHECK (page IN ('issues', 'my_issues', 'project')),
    project_id   UUID REFERENCES project(id) ON DELETE CASCADE,
    filters      JSONB NOT NULL DEFAULT '{}',
    display      JSONB NOT NULL DEFAULT '{}',
    position     FLOAT8 NOT NULL DEFAULT 0,
    shared       BOOLEAN NOT NULL DEFAULT false,
    is_default   BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT saved_view_filters_is_object CHECK (jsonb_typeof(filters) = 'object'),
    CONSTRAINT saved_view_display_is_object CHECK (jsonb_typeof(display) = 'object')
);

-- A view is uniquely named within its (workspace, page, project) scope. The
-- zero-uuid coalesce lets the page-level views (project_id IS NULL) share one
-- unique namespace distinct from per-project views.
CREATE UNIQUE INDEX idx_saved_view_unique
    ON saved_view (workspace_id, page, COALESCE(project_id, '00000000-0000-0000-0000-000000000000'::uuid), name);

CREATE INDEX idx_saved_view_workspace_page ON saved_view (workspace_id, page, position);
