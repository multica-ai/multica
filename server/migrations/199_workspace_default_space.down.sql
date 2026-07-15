DROP INDEX IF EXISTS uq_workspace_space_one_default;
ALTER TABLE workspace_space
    DROP CONSTRAINT IF EXISTS workspace_space_default_must_be_active;
ALTER TABLE workspace_space
    DROP COLUMN IF EXISTS is_default;
