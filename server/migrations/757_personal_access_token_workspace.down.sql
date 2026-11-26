-- Without the column a workspace token would read as an account-wide one, so
-- the rows go with it.
DELETE FROM personal_access_token WHERE workspace_id IS NOT NULL;
ALTER TABLE personal_access_token
    DROP COLUMN IF EXISTS workspace_id;
