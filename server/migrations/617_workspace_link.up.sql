-- Workspace Links: admin-configured shortcuts to the team's external tools,
-- displayed in workspace and project headers. Each link stores its label,
-- icon, URL template, opening mode, and manual position within one scope.
--
-- Scope is represented by project_id:
--   - NULL: workspace-level link, available across workspace headers.
--   - Non-NULL: project-level link, displayed before workspace links on that
--     project's page. The API validates that the project belongs to the workspace.
--
-- There are deliberately no foreign keys or cascading deletes. Relationships
-- are enforced in application code, and project/workspace deletion removes
-- dependent links in the same transaction as the existing cleanup path.
--
-- Deliberately absent columns:
--   - Home page / default landing: home page comes separately; a shortcut does
--     not change where workspace navigation starts.
--   - Credentials / tokens / identity: external pages receive only a
--     client-expanded URL, not Multica authentication or request headers.
--   - Proxy settings: embedded pages load directly in a sandboxed iframe;
--     Multica does not proxy content or bypass a site's framing policy.
--
-- The scope/position index lives in migration 618 so its CONCURRENTLY build
-- remains a separate single-statement migration.
CREATE TABLE workspace_link (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID,
    name TEXT NOT NULL,
    icon TEXT NOT NULL DEFAULT '',
    url_template TEXT NOT NULL,
    open_mode TEXT NOT NULL DEFAULT 'embed' CHECK (open_mode IN ('embed', 'new_tab')),
    allow_same_origin BOOLEAN NOT NULL DEFAULT false,
    position INT NOT NULL DEFAULT 0,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
