DELETE FROM multica_agent WHERE workspace_id IS NULL;
ALTER TABLE multica_agent ALTER COLUMN workspace_id SET NOT NULL;
ALTER TABLE multica_agent ALTER COLUMN runtime_id SET NOT NULL;
ALTER TABLE multica_agent DROP COLUMN IF EXISTS is_builtin;
