-- Reverse 101_issue_label_polish.

ALTER TABLE issue_to_label
    DROP COLUMN IF EXISTS actor_id,
    DROP COLUMN IF EXISTS actor_type,
    DROP COLUMN IF EXISTS assigned_at;

ALTER TABLE issue_label
    DROP CONSTRAINT IF EXISTS issue_label_color_check;

ALTER TABLE issue_label
    DROP COLUMN IF EXISTS creator_id,
    DROP COLUMN IF EXISTS creator_type,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at;

DROP INDEX IF EXISTS idx_issue_to_label_label;
DROP INDEX IF EXISTS uq_issue_label_workspace_name_ci;
