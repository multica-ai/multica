DROP TABLE IF EXISTS issue_external_identity;

ALTER TABLE issue
    DROP CONSTRAINT IF EXISTS uq_issue_workspace_id;
