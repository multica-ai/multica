-- Backing index for project_doc's primary key, attached in 620.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS project_doc_pkey_uidx
    ON project_doc (project_id, doc_id);
