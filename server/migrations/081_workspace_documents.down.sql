ALTER TABLE workspace DROP COLUMN IF EXISTS documents_agent_write_mode;

DROP INDEX IF EXISTS idx_issue_doc_link_document;
DROP INDEX IF EXISTS idx_issue_doc_link_issue;
DROP TABLE IF EXISTS issue_document_link;

ALTER TABLE workspace_document DROP CONSTRAINT IF EXISTS fk_workspace_document_current_revision;

DROP INDEX IF EXISTS idx_workspace_doc_rev_doc;
DROP TABLE IF EXISTS workspace_document_revision;

DROP INDEX IF EXISTS idx_workspace_doc_content_fts;
DROP INDEX IF EXISTS idx_workspace_doc_path_trgm;
DROP INDEX IF EXISTS idx_workspace_doc_tags;
DROP INDEX IF EXISTS idx_workspace_doc_pinned;
DROP INDEX IF EXISTS idx_workspace_doc_workspace_active;
DROP TABLE IF EXISTS workspace_document;

DROP EXTENSION IF EXISTS pg_trgm;
