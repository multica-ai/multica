ALTER TABLE project
    DROP COLUMN IF EXISTS issue_prefix,
    DROP COLUMN IF EXISTS issue_counter;
