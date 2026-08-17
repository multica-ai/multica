ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_properties_size_limit;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_properties_is_object;
ALTER TABLE issue DROP COLUMN IF EXISTS properties;

DROP INDEX IF EXISTS idx_issue_property_workspace;
DROP INDEX IF EXISTS idx_issue_property_ws_name;
DROP TABLE IF EXISTS issue_property;
