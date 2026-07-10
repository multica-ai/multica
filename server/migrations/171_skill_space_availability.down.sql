DROP TABLE IF EXISTS skill_available_space;
ALTER TABLE skill DROP CONSTRAINT IF EXISTS skill_workspace_id_id_unique;
ALTER TABLE skill DROP COLUMN IF EXISTS availability_mode;
