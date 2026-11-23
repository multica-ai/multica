CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS doc_revision_pkey_idx ON doc_revision (doc_id, revision);
