DROP INDEX IF EXISTS idx_multica_user_casdoor_universal_id;
DROP INDEX IF EXISTS idx_multica_member_workspace_status;
DROP INDEX IF EXISTS idx_multica_member_workspace_external_universal;

ALTER TABLE multica_member
    DROP CONSTRAINT IF EXISTS multica_member_status_check,
    DROP CONSTRAINT IF EXISTS multica_member_source_check,
    DROP COLUMN IF EXISTS last_synced_at,
    DROP COLUMN IF EXISTS dept_user_status,
    DROP COLUMN IF EXISTS is_main_department,
    DROP COLUMN IF EXISTS position,
    DROP COLUMN IF EXISTS dept_path,
    DROP COLUMN IF EXISTS dept_name,
    DROP COLUMN IF EXISTS dept_id,
    DROP COLUMN IF EXISTS org_display_name,
    DROP COLUMN IF EXISTS employee_id,
    DROP COLUMN IF EXISTS external_universal_id,
    DROP COLUMN IF EXISTS external_user_id,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS source;

ALTER TABLE multica_member
    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE multica_user
    DROP COLUMN IF EXISTS casdoor_universal_id;
