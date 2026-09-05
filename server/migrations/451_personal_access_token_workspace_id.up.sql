ALTER TABLE personal_access_token ADD COLUMN workspace_id UUID REFERENCES workspace(id) ON DELETE CASCADE;
CREATE INDEX idx_pat_workspace ON personal_access_token(workspace_id, revoked) WHERE workspace_id IS NOT NULL;
