DROP INDEX IF EXISTS issue_label_workspace_name_lower_idx;
ALTER TABLE issue_label
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at;
