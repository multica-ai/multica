CREATE UNIQUE INDEX CONCURRENTLY workflow_version_revision_idx ON workflow_version (workflow_id, revision);
