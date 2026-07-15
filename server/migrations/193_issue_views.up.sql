-- Saved issue views are named, server-backed snapshots of issue-list state.
-- Relationships are maintained by application cleanup rather than database
-- cascades so delete behavior stays explicit and reviewable.
CREATE TABLE issue_view (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    creator_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    icon TEXT,
    color TEXT,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project', 'my')),
    scope_id UUID,
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'workspace')),
    definition JSONB NOT NULL CHECK (jsonb_typeof(definition) = 'object'),
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type = 'project' AND scope_id IS NOT NULL)
        OR (scope_type IN ('workspace', 'my') AND scope_id IS NULL)
    ),
    CHECK (scope_type <> 'my' OR visibility = 'private')
);

-- A default view is a per-user preference for one surface. scope_id uses the
-- workspace id for workspace views, the user id for My Issues, and the project
-- id for project views so the composite primary key never depends on NULL.
CREATE TABLE issue_view_preference (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project', 'my')),
    scope_id UUID NOT NULL,
    default_view_id UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id, scope_type, scope_id)
);

CREATE INDEX idx_issue_view_preference_default
    ON issue_view_preference (workspace_id, default_view_id);

-- Older clients only understand issue/project pins. The API capability gate
-- keeps view pins out of their responses while newer clients opt in.
ALTER TABLE pinned_item DROP CONSTRAINT IF EXISTS pinned_item_item_type_check;
ALTER TABLE pinned_item
    ADD CONSTRAINT pinned_item_item_type_check
    CHECK (item_type IN ('issue', 'project', 'view'));
