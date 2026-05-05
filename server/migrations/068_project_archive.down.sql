DROP INDEX IF EXISTS idx_project_active;
ALTER TABLE project DROP COLUMN IF EXISTS archived_by;
ALTER TABLE project DROP COLUMN IF EXISTS archived_at;
