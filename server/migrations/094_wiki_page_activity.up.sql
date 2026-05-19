CREATE TABLE wiki_page_activity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    page_id UUID NOT NULL REFERENCES wiki_page(id) ON DELETE CASCADE,
    actor_id UUID REFERENCES "user"(id),
    action TEXT NOT NULL,
    details JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wiki_page_activity_page ON wiki_page_activity(page_id, created_at DESC);
CREATE INDEX idx_wiki_page_activity_workspace ON wiki_page_activity(workspace_id, created_at DESC);
