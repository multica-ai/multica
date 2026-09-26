-- Backing index for issue_workflow's primary key, attached in 553.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_workflow_pkey_uidx
    ON issue_workflow (id);
