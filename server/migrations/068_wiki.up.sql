CREATE TABLE wiki_page (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    slug TEXT,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wiki_page_workspace ON wiki_page(workspace_id);
CREATE UNIQUE INDEX idx_wiki_page_workspace_slug ON wiki_page(workspace_id, slug) WHERE slug IS NOT NULL;
