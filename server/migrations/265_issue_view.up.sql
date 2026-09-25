-- Saved issue views: named, server-backed filter definitions with display
-- defaults. `query` (filters) is the shared identity of the view; `display`
-- (layout/grouping/sort/card fields) only seeds the first open and is
-- overridden per user locally. No FKs or cascades by repository policy —
-- lifecycle cleanup is handled in application transactions (project delete,
-- member removal).
CREATE TABLE issue_view (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    owner_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'my', 'project')),
    scope_id UUID,
    scope_variant TEXT CHECK (scope_variant IN ('assigned', 'created', 'involved', 'any')),
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'workspace')),
    definition_version INTEGER NOT NULL DEFAULT 1,
    query JSONB NOT NULL CHECK (jsonb_typeof(query) = 'object'),
    display JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(display) = 'object'),
    revision INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- scope_id carries the project id for project views and must be absent
    -- elsewhere; scope_variant is meaningful only for My Issues views.
    CHECK (
        (scope_type = 'project' AND scope_id IS NOT NULL)
        OR (scope_type IN ('workspace', 'my') AND scope_id IS NULL)
    ),
    CHECK (
        (scope_type = 'my' AND scope_variant IS NOT NULL)
        OR (scope_type <> 'my' AND scope_variant IS NULL)
    ),
    -- My Issues is inherently personal; sharing it would leak a per-user
    -- perspective that renders differently for every member.
    CHECK (scope_type <> 'my' OR visibility = 'private')
);
