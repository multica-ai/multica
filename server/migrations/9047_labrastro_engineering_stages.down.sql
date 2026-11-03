BEGIN;
ALTER TABLE project_engineering DROP CONSTRAINT IF EXISTS project_engineering_stages_check;
ALTER TABLE project_engineering DROP COLUMN IF EXISTS stages;
COMMIT;
