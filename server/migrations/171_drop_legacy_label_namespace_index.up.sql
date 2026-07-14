-- The replacement namespace index is created in migration 167 before this
-- legacy index is removed. Keep this a single statement for online safety.
DROP INDEX CONCURRENTLY IF EXISTS issue_label_workspace_name_lower_idx;
