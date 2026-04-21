CREATE TABLE workspace_column_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')),
    instructions TEXT NOT NULL DEFAULT '',
    allowed_transitions TEXT[] NOT NULL DEFAULT '{}' CHECK (
        allowed_transitions <@ ARRAY['backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled']::TEXT[]
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(workspace_id, status)
);
