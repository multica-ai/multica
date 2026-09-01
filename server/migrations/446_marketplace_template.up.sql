CREATE TABLE marketplace_template (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    source_workspace_id UUID,
    created_by UUID,
    source_type TEXT NOT NULL CHECK (source_type IN ('agent', 'squad')),
    source_id UUID,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT '{}',
    visibility TEXT NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'workspace', 'public')),
    image_url TEXT,
    snapshot_version INTEGER NOT NULL DEFAULT 1,
    snapshot JSONB NOT NULL,
    applied_count BIGINT NOT NULL DEFAULT 0,
    featured_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
