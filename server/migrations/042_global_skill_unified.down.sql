DROP INDEX IF EXISTS skill_global_runtime_name_unique;
DROP INDEX IF EXISTS skill_workspace_name_unique;
DROP INDEX IF EXISTS idx_skill_runtime;

ALTER TABLE skill
    DROP COLUMN IF EXISTS is_global,
    DROP COLUMN IF EXISTS runtime_id;

ALTER TABLE skill ADD CONSTRAINT skill_workspace_id_name_key UNIQUE (workspace_id, name);
