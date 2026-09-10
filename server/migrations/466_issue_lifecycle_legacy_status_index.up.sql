CREATE UNIQUE INDEX CONCURRENTLY idx_issue_lifecycle_status_legacy_key ON issue_lifecycle_status(lifecycle_id, legacy_status_key) WHERE legacy_status_key IS NOT NULL;
