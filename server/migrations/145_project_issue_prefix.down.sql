ALTER TABLE project
    DROP CONSTRAINT IF EXISTS project_issue_prefix_format,
    DROP COLUMN IF EXISTS issue_prefix;
