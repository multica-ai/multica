-- Backing index for issue_workflow's primary key, attached in 575.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_workflow_pkey_uidx
    ON issue_workflow (id);
