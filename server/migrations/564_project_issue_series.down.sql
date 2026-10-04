DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM issue GROUP BY workspace_id, number HAVING COUNT(DISTINCT identifier_prefix) > 1) THEN
        RAISE EXCEPTION 'cannot remove project issue series: multiple prefixes reuse an issue number';
    END IF;
END $$;
DROP INDEX IF EXISTS uq_issue_workspace_prefix_number;
DROP INDEX IF EXISTS uq_issue_identifier_series_workspace_prefix;
DROP TABLE IF EXISTS issue_identifier_series;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_identifier_prefix_format;
ALTER TABLE project DROP CONSTRAINT IF EXISTS project_issue_prefix_format;
ALTER TABLE issue DROP COLUMN IF EXISTS identifier_prefix;
ALTER TABLE project DROP COLUMN IF EXISTS issue_prefix;
ALTER TABLE issue ADD CONSTRAINT uq_issue_workspace_number UNIQUE (workspace_id, number);
