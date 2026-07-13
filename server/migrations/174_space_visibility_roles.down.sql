DROP INDEX IF EXISTS idx_workspace_space_visibility;

ALTER TABLE workspace_space
    DROP CONSTRAINT IF EXISTS workspace_space_default_must_be_open;

UPDATE workspace_space_member
SET role = 'member'
WHERE role IN ('admin', 'guest');

ALTER TABLE workspace_space_member
    DROP CONSTRAINT workspace_space_member_role_check;

ALTER TABLE workspace_space_member
    ADD CONSTRAINT workspace_space_member_role_check
        CHECK (role IN ('lead', 'member'));

ALTER TABLE workspace_space
    DROP COLUMN IF EXISTS visibility;
