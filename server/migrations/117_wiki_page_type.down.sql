DROP INDEX IF EXISTS idx_wiki_page_workspace_type;
ALTER TABLE wiki_page DROP COLUMN IF EXISTS type;
