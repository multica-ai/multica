-- Remove retained plan version uniqueness and history ordering support.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_project_version_key;
