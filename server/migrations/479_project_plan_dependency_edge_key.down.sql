-- Remove dependency edge uniqueness across nullable endpoints.
DROP INDEX CONCURRENTLY IF EXISTS project_plan_dependency_edge_key;
