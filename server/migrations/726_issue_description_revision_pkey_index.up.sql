CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_description_revision_pkey_idx ON issue_description_revision (issue_id, revision);
