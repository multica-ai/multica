DROP INDEX IF EXISTS idx_pat_workspace;
ALTER TABLE personal_access_token DROP COLUMN IF EXISTS workspace_id;
