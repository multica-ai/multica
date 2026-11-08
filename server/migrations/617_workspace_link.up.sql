-- Admin-configured quick links. NULL project_id denotes workspace scope.
-- Relationships and deletion cleanup are enforced in application code, not FKs.
-- Deliberately absent: home/default landing, credentials, tokens, and proxy settings.
-- External pages receive only a client-expanded URL and an explicit sandbox policy.
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
