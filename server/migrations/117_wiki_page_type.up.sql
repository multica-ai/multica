ALTER TABLE wiki_page ADD COLUMN type TEXT NOT NULL DEFAULT 'page';
CREATE INDEX idx_wiki_page_workspace_type ON wiki_page(workspace_id, type);
