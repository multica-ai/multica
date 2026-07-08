ALTER TABLE project
DROP CONSTRAINT IF EXISTS project_workdir_policy_check;

ALTER TABLE project
DROP COLUMN IF EXISTS canonical_workdir,
DROP COLUMN IF EXISTS workdir_policy;
