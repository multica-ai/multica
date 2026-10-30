-- Doc side panel lists the projects a doc is linked to.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_project_doc_doc
    ON project_doc (doc_id);
