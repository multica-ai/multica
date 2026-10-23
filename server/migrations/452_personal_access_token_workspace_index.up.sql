CREATE INDEX CONCURRENTLY idx_pat_workspace ON personal_access_token(workspace_id, revoked) WHERE workspace_id IS NOT NULL;
