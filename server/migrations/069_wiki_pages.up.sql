CREATE TABLE wiki_page (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES wiki_page(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    position DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_by UUID REFERENCES "user"(id),
    updated_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wiki_page_workspace_parent_position
    ON wiki_page(workspace_id, parent_id, position);

CREATE UNIQUE INDEX uniq_wiki_page_root_slug
    ON wiki_page(workspace_id, slug)
    WHERE parent_id IS NULL;

CREATE UNIQUE INDEX uniq_wiki_page_child_slug
    ON wiki_page(workspace_id, parent_id, slug)
    WHERE parent_id IS NOT NULL;

INSERT INTO wiki_page (workspace_id, title, slug, content, position)
SELECT id, 'Wiki', 'home', wiki_content, 0
FROM workspace
WHERE wiki_content IS NOT NULL AND btrim(wiki_content) <> '';
